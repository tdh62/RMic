package cool.cnb.rmic

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import org.json.JSONObject
import java.security.MessageDigest
import java.security.SecureRandom
import java.security.cert.CertificateException
import java.security.cert.X509Certificate
import java.util.Base64
import java.util.concurrent.TimeUnit
import javax.net.ssl.SSLContext
import javax.net.ssl.SSLSocket
import javax.net.ssl.X509TrustManager
import okhttp3.HttpUrl.Companion.toHttpUrl

object ServerTrust {
    fun pin(certificate: X509Certificate): String = "sha256/" + Base64.getEncoder().encodeToString(
        MessageDigest.getInstance("SHA-256").digest(certificate.publicKey.encoded)
    )

    // This connection only inspects a certificate. No HTTP or credential is sent before confirmation.
    suspend fun inspect(origin: String): String = withContext(Dispatchers.IO) {
        val url = origin.toHttpUrl()
        val inspectionTrust = object : X509TrustManager {
            override fun getAcceptedIssuers(): Array<X509Certificate> = emptyArray()
            override fun checkClientTrusted(chain: Array<X509Certificate>, authType: String) =
                throw CertificateException("Client certificates are unsupported")
            override fun checkServerTrusted(chain: Array<X509Certificate>, authType: String) {
                chain.first().checkValidity()
            }
        }
        val context = SSLContext.getInstance("TLS").apply {
            init(null, arrayOf(inspectionTrust), SecureRandom())
        }
        (context.socketFactory.createSocket() as SSLSocket).use { socket ->
            socket.soTimeout = 10_000
            socket.connect(java.net.InetSocketAddress(url.host, url.port), 10_000)
            socket.startHandshake()
            pin(socket.session.peerCertificates.first() as X509Certificate)
        }
    }

    fun client(expectedPin: String): OkHttpClient {
        require(expectedPin.matches(Regex("sha256/[A-Za-z0-9+/]{43}="))) { "无效的电脑证书指纹" }
        val trust = object : X509TrustManager {
            override fun getAcceptedIssuers(): Array<X509Certificate> = emptyArray()
            override fun checkClientTrusted(chain: Array<X509Certificate>, authType: String) =
                throw CertificateException("Client certificates are unsupported")
            override fun checkServerTrusted(chain: Array<X509Certificate>, authType: String) {
                if (chain.isEmpty()) throw CertificateException("Missing certificate")
                chain.first().checkValidity()
                if (pin(chain.first()) != expectedPin) {
                    throw CertificateException("电脑证书密钥已变化，请重新核对并配对")
                }
            }
        }
        val context = SSLContext.getInstance("TLS").apply { init(null, arrayOf(trust), SecureRandom()) }
        return OkHttpClient.Builder()
            .sslSocketFactory(context.socketFactory, trust)
            .followRedirects(false).followSslRedirects(false)
            .connectTimeout(10, TimeUnit.SECONDS).readTimeout(15, TimeUnit.SECONDS)
            .callTimeout(20, TimeUnit.SECONDS)
            .build()
    }
}

class RmicApi(val profile: ConnectionProfile) : AutoCloseable {
    private val client = ServerTrust.client(profile.pin)
    private val closed = java.util.concurrent.atomic.AtomicBoolean(false)

    suspend fun request(
        path: String, method: String = "GET", body: JSONObject? = null,
        headers: Map<String, String> = emptyMap(),
    ): JSONObject = withContext(Dispatchers.IO) {
        val builder = Request.Builder().url(profile.origin + path)
        if (profile.token.isNotEmpty()) builder.header("Authorization", "Bearer ${profile.token}")
        headers.forEach { (name, value) -> builder.header(name, value) }
        builder.method(method, if (method in listOf("POST", "PUT")) {
            (body ?: JSONObject()).toString().toRequestBody("application/json".toMediaType())
        } else null)
        client.newCall(builder.build()).execute().use { response ->
            val text = response.body?.string().orEmpty()
            if (!response.isSuccessful) {
                throw ApiException(response.code, response.header("X-RMic-Error").orEmpty(),
                    text.take(300).ifBlank { "电脑响应错误：${response.code}" })
            }
            if (text.isBlank()) JSONObject() else JSONObject(text)
        }
    }

    suspend fun status() = ServerStatus.fromJson(request("/api/status"))
    suspend fun deleteSession(id: String) { if (id.isNotEmpty()) request("/api/session/$id", "DELETE") }

    suspend fun shortcutTicket(sessionId: String, revision: Long): String =
        request("/api/shortcuts/ticket", "POST", headers = mapOf(
            "X-RMic-Session" to sessionId, "X-RMic-Shortcuts" to revision.toString()
        )).getString("ticket")

    fun shortcutSocket(ticket: String, listener: okhttp3.WebSocketListener): okhttp3.WebSocket {
        check(!closed.get()) { "控制连接已关闭" }
        val url = (profile.origin + "/api/shortcuts/control").toHttpUrl().newBuilder()
            .addQueryParameter("ticket", ticket).build()
        return client.newWebSocket(Request.Builder().url(url).header("Origin", profile.origin).build(), listener)
    }
    override fun close() {
        if (!closed.compareAndSet(false, true)) return
        val executor = client.dispatcher.executorService
        executor.execute {
            client.dispatcher.cancelAll()
            client.connectionPool.evictAll()
        }
        executor.shutdown()
    }
}
