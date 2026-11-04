package cool.cnb.rmic

import android.content.Context
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import org.json.JSONObject
import org.junit.After
import org.junit.Assert.*
import org.junit.Test
import org.junit.runner.RunWith
import java.security.GeneralSecurityException

@RunWith(AndroidJUnit4::class)
class ProfileStoreTest {
    private val instrumentation = InstrumentationRegistry.getInstrumentation()
    private val context = instrumentation.targetContext
    private val preferencesName = "rmic-test-multiple-servers"
    private val preferences = context.getSharedPreferences(preferencesName, Context.MODE_PRIVATE)
    private val store = ProfileStore(context, preferencesName)
    private val first = ConnectionProfile("https://192.168.1.10:8443", "sha256/" + "A".repeat(43) + "=", "a".repeat(48))
    private val second = ConnectionProfile("https://192.168.1.10:9443", "sha256/" + "B".repeat(43) + "=", "b".repeat(48))

    @After fun cleanup() { store.clear() }

    @Test fun serversPersistIndependentlyAndRevokingOneKeepsTheOther() {
        store.save(first, true)
        store.save(second, true)
        assertEquals(second, store.load())
        assertEquals(first, store.load(first.origin))
        store.select(first.origin)
        store.save(second.copy(token = ""), false, select = false)
        val reopened = ProfileStore(context, preferencesName)
        assertEquals(first, reopened.load())
        assertEquals(second.copy(token = ""), reopened.load(second.origin))
        assertEquals(listOf(first.origin, second.origin), reopened.origins())
        reopened.remove(second.origin)
        assertEquals(first, reopened.load())
        assertEquals(listOf(first.origin), reopened.origins())
        assertFalse(preferences.all.toString().contains(first.token))
    }

    @Test fun legacyEncryptedAuthorizationMigratesWithoutRePairing() {
        store.save(first, true)
        val encrypted = JSONObject(checkNotNull(preferences.getString("servers", null))).getJSONObject(first.origin).getJSONObject("credential")
        assertTrue(preferences.edit().clear().putString("origin", first.origin).putString("pin", first.pin)
            .putString("credential", encrypted.toString()).commit())
        val migrated = ProfileStore(context, preferencesName)
        assertEquals(first, migrated.load())
        assertEquals(first.origin, migrated.selectedOrigin())
        assertEquals(2, preferences.getInt("version", 0))
        assertFalse(preferences.contains("credential"))
        migrated.save(second, true)
        assertEquals(first, migrated.load(first.origin))
        assertEquals(second, migrated.load())
    }

    @Test fun credentialCannotBeMovedToAnotherServerAndDoesNotBreakOtherRecords() {
        store.save(first, true)
        store.save(second, true)
        val servers = JSONObject(checkNotNull(preferences.getString("servers", null)))
        servers.getJSONObject(second.origin).put("pin", first.pin)
            .put("credential", servers.getJSONObject(first.origin).getJSONObject("credential"))
        assertTrue(preferences.edit().putString("servers", servers.toString()).commit())
        assertThrows(GeneralSecurityException::class.java) { store.load(second.origin) }
        assertEquals(first, store.load(first.origin))
        assertEquals(2, store.origins().size)
        store.remove(second.origin)
        store.select(first.origin)
        assertEquals(first, store.load())
    }

    @Test fun temporaryCredentialsSurviveSwitchingOnlyInTheCurrentProcess() {
        store.save(first, false)
        store.save(second, false)
        assertEquals("", store.load(first.origin)!!.token)
        assertEquals("", store.load(second.origin)!!.token)
        instrumentation.runOnMainSync {
            val previous = ClientRuntime.profile
            try {
                ClientRuntime.profile = first
                ClientRuntime.profile = second
                assertEquals(first, ClientRuntime.cachedProfile(first.origin))
                ClientRuntime.updateProfile(first.copy(token = ""))
                assertEquals(second, ClientRuntime.profile)
                assertEquals("", ClientRuntime.cachedProfile(first.origin)!!.token)
                ClientRuntime.forgetProfile(first.origin)
                assertEquals(second, ClientRuntime.profile)
            } finally {
                ClientRuntime.forgetProfile(first.origin)
                ClientRuntime.forgetProfile(second.origin)
                ClientRuntime.profile = previous
            }
        }
    }
}
