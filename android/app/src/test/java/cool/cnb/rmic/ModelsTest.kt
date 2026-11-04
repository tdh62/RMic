package cool.cnb.rmic

import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Test

class ModelsTest {
    @Test fun acceptsIpv6AndDiscardsBootstrapFragment() {
        assertEquals("https://[fd7a:115c:a1e0::1234]:8443", normalizeOrigin("https://[fd7a:115c:a1e0::1234]:8443/#token=secret"))
        assertEquals("https://192.168.1.10:8443", normalizeOrigin(" https://192.168.1.10:8443/ "))
    }

    @Test fun rejectsInsecureOrAmbiguousAddresses() {
        listOf("http://192.168.1.10:8443/", "https://user:password@localhost/", "https://localhost/api/status", "https://localhost/?token=secret").forEach {
            try { normalizeOrigin(it); fail("Must reject $it") } catch (_: IllegalArgumentException) {}
        }
    }

    @Test fun readsServerCapabilitiesWithoutEnablingUnavailableModes() {
        val status = ServerStatus.fromJson(JSONObject("""
            {"instance_id":"instance","config_revision":4,"state":"waiting","error":"",
             "modes":["playback"],"default_mode":"playback","capture_channels":8,
             "default_quality":{"id":"ultra","bitrate":384000,"channels":2,"playback_only":true},
             "qualities":[{"id":"balanced","bitrate":96000,"channels":2},
                          {"id":"ultra","bitrate":384000,"channels":2,"playback_only":true}],
             "receive":{"error":""},"send":{"error":"采集设备失效"}}
        """))
        assertEquals(listOf(AudioMode.PLAYBACK), status.modes)
        assertEquals(AudioMode.PLAYBACK, status.defaultMode)
        assertEquals("ultra", status.defaultQuality)
        assertTrue(status.qualities.last().playbackOnly)
        assertEquals("采集设备失效", status.directionError)
        assertEquals(4L, status.revision)
        assertNull(status.shortcuts)
    }

    @Test fun retriesConfigurationChangesButStopsForOwnershipAndAuthorization() {
        assertFalse(ApiException(409, "configuration_changed", "changed").terminal)
        assertFalse(ApiException(409, "session_replaced", "changed").terminal)
        assertFalse(ApiException(503, "", "offline").terminal)
        assertTrue(ApiException(409, "session_busy", "busy").terminal)
        assertTrue(ApiException(409, "computer_disconnected", "stopped").terminal)
        assertTrue(ApiException(401, "", "revoked").terminal)
        assertTrue(ApiException(422, "mode_unavailable", "disabled").terminal)
    }
    @Test fun optionalShortcutCapabilitiesPreserveSlotIdsAndModes() {
        val config = ShortcutConfig.fromJson(JSONObject("""
            {"revision":7,"buttons":[{"id":"1","name":"按住说话","mode":"hold"},
             {"id":"3","name":"固定回复","mode":"tap"}],"error":""}
        """))
        assertEquals(7L, config.revision)
        assertEquals(listOf("1", "3"), config.buttons.map { it.id })
        assertEquals(listOf("hold", "tap"), config.buttons.map { it.mode })
        listOf(
            """{"revision":1,"buttons":[]}""",
            """{"revision":1,"buttons":[{"id":"1","name":"a","mode":"script"}]}""",
            """{"revision":1,"buttons":[{"id":"1","name":"a","mode":"tap"},{"id":"1","name":"b","mode":"hold"}]}"""
        ).forEach { json ->
            try { ShortcutConfig.fromJson(JSONObject(json)); fail("Invalid shortcut capabilities accepted") }
            catch (_: IllegalArgumentException) { }
        }
    }
}
