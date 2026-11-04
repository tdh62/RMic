package cool.cnb.rmic

import kotlinx.coroutines.runBlocking
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import okhttp3.tls.HandshakeCertificates
import okhttp3.tls.HeldCertificate
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Test
import java.io.IOException
import java.util.concurrent.TimeUnit

class RmicApiTest {
    private fun server(certificate: HeldCertificate): MockWebServer = MockWebServer().apply {
        val tls = HandshakeCertificates.Builder().heldCertificate(certificate).build()
        useHttps(tls.sslSocketFactory(), false)
        start()
    }

    @Test fun pinnedSelfSignedServerReceivesCredentialAndRevision() = runBlocking {
        val certificate = HeldCertificate.Builder().addSubjectAlternativeName("localhost").build()
        server(certificate).use { server ->
            server.enqueue(MockResponse().setBody("{\"id\":\"session-id\"}"))
            val profile = ConnectionProfile(server.url("/").toString().removeSuffix("/"), ServerTrust.pin(certificate.certificate), "a".repeat(48))
            RmicApi(profile).use { api ->
                val response = api.request("/api/session", "POST", JSONObject().put("mode", "mic"),
                    mapOf("X-RMic-Config" to "7", "X-RMic-Instance" to "instance"))
                assertEquals("session-id", response.getString("id"))
            }
            val request = server.takeRequest(2, TimeUnit.SECONDS)!!
            assertEquals("Bearer ${profile.token}", request.getHeader("Authorization"))
            assertEquals("7", request.getHeader("X-RMic-Config"))
            assertEquals("instance", request.getHeader("X-RMic-Instance"))
            assertEquals("mic", JSONObject(request.body.readUtf8()).getString("mode"))
        }
    }

    @Test fun wrongPinIsRejectedBeforeSendingCredential() = runBlocking {
        val certificate = HeldCertificate.Builder().addSubjectAlternativeName("localhost").build()
        server(certificate).use { server ->
            val other = HeldCertificate.Builder().build()
            RmicApi(ConnectionProfile(server.url("/").toString().removeSuffix("/"), ServerTrust.pin(other.certificate), "secret")).use { api ->
                try { api.status(); fail("Wrong pin must fail") } catch (_: IOException) {}
            }
            assertNull(server.takeRequest(100, TimeUnit.MILLISECONDS))
        }
    }

    @Test fun matchingPinDoesNotBypassHostnameVerification() = runBlocking {
        val certificate = HeldCertificate.Builder().addSubjectAlternativeName("different-host.invalid").build()
        server(certificate).use { server ->
            RmicApi(ConnectionProfile(server.url("/").toString().removeSuffix("/"), ServerTrust.pin(certificate.certificate), "secret")).use { api ->
                try { api.status(); fail("Wrong hostname must fail") } catch (_: IOException) {}
            }
            assertNull(server.takeRequest(100, TimeUnit.MILLISECONDS))
        }
    }

    @Test fun redirectsCannotForwardAuthorization() = runBlocking {
        val certificate = HeldCertificate.Builder().addSubjectAlternativeName("localhost").build()
        server(certificate).use { server ->
            server.enqueue(MockResponse().setResponseCode(302).setHeader("Location", "https://example.com/collect"))
            RmicApi(ConnectionProfile(server.url("/").toString().removeSuffix("/"), ServerTrust.pin(certificate.certificate), "secret")).use { api ->
                try { api.status(); fail("Redirect must fail") }
                catch (error: ApiException) { assertEquals(302, error.status) }
            }
            assertEquals(1, server.requestCount)
        }
    }

    @Test fun inspectionSendsNoHttpRequest() = runBlocking {
        val certificate = HeldCertificate.Builder().addSubjectAlternativeName("localhost").build()
        server(certificate).use { server ->
            val pin = ServerTrust.inspect(server.url("/").toString())
            assertEquals(ServerTrust.pin(certificate.certificate), pin)
            assertNull(server.takeRequest(100, TimeUnit.MILLISECONDS))
        }
    }

    @Test fun revokedAuthorizationRetainsTerminalErrorCode() = runBlocking {
        val certificate = HeldCertificate.Builder().addSubjectAlternativeName("localhost").build()
        server(certificate).use { server ->
            server.enqueue(MockResponse().setResponseCode(401).setHeader("X-RMic-Error", "unauthorized").setBody("授权已失效"))
            RmicApi(ConnectionProfile(server.url("/").toString().removeSuffix("/"), ServerTrust.pin(certificate.certificate), "secret")).use { api ->
                try { api.status(); fail("Revoked token must fail") }
                catch (error: ApiException) {
                    assertEquals("unauthorized", error.code)
                    assertTrue(error.terminal)
                    assertEquals("授权已失效", error.message)
                }
            }
        }
    }
    @Test fun shortcutSocketRejectsChangedCertificateBeforeSendingTicket() {
        val certificate = HeldCertificate.Builder().addSubjectAlternativeName("localhost").build()
        server(certificate).use { server ->
            val other = HeldCertificate.Builder().build()
            val failed = java.util.concurrent.CountDownLatch(1)
            RmicApi(ConnectionProfile(server.url("/").toString().removeSuffix("/"), ServerTrust.pin(other.certificate), "secret")).use { api ->
                val socket = api.shortcutSocket("sensitive-ticket", object : okhttp3.WebSocketListener() {
                    override fun onFailure(webSocket: okhttp3.WebSocket, t: Throwable, response: okhttp3.Response?) { failed.countDown() }
                })
                assertTrue(failed.await(5, TimeUnit.SECONDS))
                assertNull(server.takeRequest(100, TimeUnit.MILLISECONDS))
                socket.cancel()
            }
        }
    }
}
