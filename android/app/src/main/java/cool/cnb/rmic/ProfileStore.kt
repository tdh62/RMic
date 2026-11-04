package cool.cnb.rmic

import android.content.Context
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import android.util.Base64
import org.json.JSONObject
import java.security.KeyStore
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec

class ProfileStore(context: Context, preferencesName: String = "rmic-profile") {
    private val preferences = context.getSharedPreferences(preferencesName, Context.MODE_PRIVATE)
    private val alias = "rmic-device-authorization-v1"

    private fun key(): SecretKey {
        val store = KeyStore.getInstance("AndroidKeyStore").apply { load(null) }
        (store.getKey(alias, null) as? SecretKey)?.let { return it }
        return KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, "AndroidKeyStore").apply {
            init(KeyGenParameterSpec.Builder(alias,
                KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT)
                .setBlockModes(KeyProperties.BLOCK_MODE_GCM)
                .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE).build())
        }.generateKey()
    }

    private fun records(): JSONObject {
        preferences.getString("servers", null)?.let {
            check(preferences.getInt("version", 0) == 2) { "不支持的服务器记录版本" }
            return JSONObject(it)
        }
        val servers = JSONObject()
        val origin = preferences.getString("origin", null) ?: return servers
        val pin = checkNotNull(preferences.getString("pin", null)) { "旧电脑证书记录不完整" }
        val record = JSONObject().put("pin", pin)
        preferences.getString("credential", null)?.let { record.put("credential", JSONObject(it)) }
        servers.put(origin, record)
        // Keep the encrypted payload and its original AAD, so existing authorizations survive migration.
        check(preferences.edit().putInt("version", 2).putString("servers", servers.toString())
            .putString("selected_origin", origin).remove("origin").remove("pin").remove("credential").commit()) {
            "无法迁移已有设备授权"
        }
        return servers
    }

    fun origins(): List<String> = synchronized(lock) { records().keys().asSequence().toList().sorted() }

    fun selectedOrigin(): String? = synchronized(lock) {
        records()
        preferences.getString("selected_origin", null)
    }

    fun select(origin: String) = synchronized(lock) {
        require(records().has(origin)) { "未保存此服务器" }
        check(preferences.edit().putString("selected_origin", origin).commit()) { "无法保存当前服务器" }
    }

    fun load(origin: String? = null): ConnectionProfile? = synchronized(lock) {
        val servers = records()
        val selected = origin ?: preferences.getString("selected_origin", null) ?: return@synchronized null
        val record = servers.optJSONObject(selected) ?: return@synchronized null
        val pin = record.getString("pin")
        val json = record.optJSONObject("credential") ?: return@synchronized ConnectionProfile(selected, pin, "")
        val cipher = Cipher.getInstance("AES/GCM/NoPadding")
        cipher.init(Cipher.DECRYPT_MODE, key(), GCMParameterSpec(128, Base64.decode(json.getString("iv"), Base64.NO_WRAP)))
        cipher.updateAAD("$selected\n$pin".toByteArray(Charsets.UTF_8))
        val token = cipher.doFinal(Base64.decode(json.getString("data"), Base64.NO_WRAP)).toString(Charsets.UTF_8)
        ConnectionProfile(selected, pin, token)
    }

    fun save(profile: ConnectionProfile, remember: Boolean, select: Boolean = true) = synchronized(lock) {
        require(normalizeOrigin(profile.origin) == profile.origin) { "服务器地址必须标准化" }
        val servers = records()
        val record = JSONObject().put("pin", profile.pin)
        if (remember && profile.token.isNotEmpty()) {
            val cipher = Cipher.getInstance("AES/GCM/NoPadding")
            cipher.init(Cipher.ENCRYPT_MODE, key())
            cipher.updateAAD("${profile.origin}\n${profile.pin}".toByteArray(Charsets.UTF_8))
            val encrypted = cipher.doFinal(profile.token.toByteArray(Charsets.UTF_8))
            record.put("credential", JSONObject()
                .put("iv", Base64.encodeToString(cipher.iv, Base64.NO_WRAP))
                .put("data", Base64.encodeToString(encrypted, Base64.NO_WRAP)))
        }
        servers.put(profile.origin, record)
        val editor = preferences.edit().putInt("version", 2).putString("servers", servers.toString())
        if (select) editor.putString("selected_origin", profile.origin)
        check(editor.commit()) { "无法保存设备授权" }
    }

    fun remove(origin: String) = synchronized(lock) {
        val servers = records()
        servers.remove(origin)
        val editor = preferences.edit().putInt("version", 2).putString("servers", servers.toString())
        if (preferences.getString("selected_origin", null) == origin) editor.remove("selected_origin")
        check(editor.commit()) { "无法清除此服务器的授权" }
    }

    fun clear() = synchronized(lock) {
        check(preferences.edit().clear().commit()) { "无法清除设备授权" }
    }

    companion object {
        private val lock = Any()
    }
}
