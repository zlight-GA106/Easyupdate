package com.zlight106.easyupdate.demo

import android.app.Activity
import android.app.AlertDialog
import android.content.Intent
import android.net.Uri
import android.os.Build
import android.os.Bundle
import android.provider.Settings
import android.view.View
import android.widget.Button
import android.widget.EditText
import android.widget.ProgressBar
import android.widget.TextView
import androidx.core.content.FileProvider
import org.json.JSONObject
import java.io.ByteArrayOutputStream
import java.io.File
import java.net.HttpURLConnection
import java.net.URL
import java.security.MessageDigest
import java.util.Locale
import java.util.UUID
import java.util.concurrent.Executors

class MainActivity : Activity() {
    private val executor = Executors.newSingleThreadExecutor()
    private val preferences by lazy { getSharedPreferences("easyupdate", MODE_PRIVATE) }
    private lateinit var server: EditText
    private lateinit var status: TextView
    private lateinit var check: Button
    private lateinit var download: Button
    private lateinit var install: Button
    private lateinit var progress: ProgressBar
    private var update: JSONObject? = null
    private var verifiedFile: File? = null
    private val currentVersion by lazy { packageManager.getPackageInfo(packageName, 0) }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_main)
        server = findViewById(R.id.server_url)
        status = findViewById(R.id.status)
        check = findViewById(R.id.check)
        download = findViewById(R.id.download)
        install = findViewById(R.id.install)
        progress = findViewById(R.id.progress)
        findViewById<TextView>(R.id.current).text = "$packageName\n${currentVersion.versionName} · ${currentVersion.versionCode}"
        server.setText(preferences.getString("server", "http://10.0.2.2:8080"))
        if (!preferences.contains("device_id")) preferences.edit().putString("device_id", UUID.randomUUID().toString()).apply()
        check.setOnClickListener { checkUpdate() }
        download.setOnClickListener { downloadUpdate() }
        install.setOnClickListener { installUpdate() }
        findViewById<Button>(R.id.later).setOnClickListener { findViewById<View>(R.id.update_panel).visibility = View.GONE }
        val startupOrigin = try { origin() } catch (_: Exception) { null }
        if (startupOrigin != null) executor.execute { try { heartbeat(startupOrigin) } catch (_: Exception) { /* Telemetry is optional. */ } }
    }

    private fun origin(): String {
        val value = server.text.toString().trim().trimEnd('/')
        val url = URL(value)
        require(url.protocol in listOf("http", "https") && url.host.isNotEmpty() && url.userInfo == null && url.query == null && url.ref == null && url.path.isEmpty()) { "Invalid server URL" }
        return value
    }

    private fun ui(action: () -> Unit) { runOnUiThread { if (!isFinishing && !isDestroyed) action() } }

    private fun connection(url: String): HttpURLConnection = (URL(url).openConnection() as HttpURLConnection).apply {
        connectTimeout = 15_000
        readTimeout = 30_000
        instanceFollowRedirects = false
        setRequestProperty("Accept", "application/json")
    }

    private fun readJSON(connection: HttpURLConnection): JSONObject {
        require(connection.responseCode in 200..299) { "Server returned HTTP ${connection.responseCode}" }
        val output = ByteArrayOutputStream()
        connection.inputStream.use { input ->
            val buffer = ByteArray(4096)
            while (true) {
                val count = input.read(buffer)
                if (count < 0) break
                require(output.size() + count <= 65_536) { "Response too large" }
                output.write(buffer, 0, count)
            }
        }
        return JSONObject(output.toString("UTF-8"))
    }

    private fun heartbeat(base: String) {
        val body = JSONObject().put("device_id", preferences.getString("device_id", ""))
            .put("package_name", packageName).put("version_name", currentVersion.versionName)
            .put("version_code", currentVersion.versionCode).toString().toByteArray(Charsets.UTF_8)
        val conn = connection("$base/api/v1/heartbeat")
        try {
            conn.requestMethod = "POST"
            conn.doOutput = true
            conn.setRequestProperty("Content-Type", "application/json")
            conn.setFixedLengthStreamingMode(body.size)
            conn.outputStream.use { it.write(body) }
            readJSON(conn)
        } finally { conn.disconnect() }
    }

    private fun checkUpdate() {
        val base = try { origin() } catch (e: Exception) { status.text = e.message; return }
        preferences.edit().putString("server", base).apply()
        check.isEnabled = false
        server.isEnabled = false
        status.text = "Checking…"
        update = null
        verifiedFile = null
        install.visibility = View.GONE
        findViewById<View>(R.id.update_panel).visibility = View.GONE
        executor.execute {
            val conn = connection("$base/api/v1/apps/$packageName/latest?version_code=${currentVersion.versionCode}")
            try {
                val result = readJSON(conn)
                require(result.getString("package_name") == packageName) { "Package mismatch" }
                if (result.getBoolean("update_available")) {
                    val version = result.getLong("version_code")
                    val size = result.getLong("size")
                    val digest = result.getString("sha256")
                    val url = URL(result.getString("download_url"))
                    val origin = URL(base)
                    require(version > currentVersion.versionCode && size in 1..536_870_912L && digest.matches(Regex("[a-fA-F0-9]{64}"))) { "Invalid update metadata" }
                    require(url.protocol == origin.protocol && url.host == origin.host && url.port == origin.port && url.userInfo == null) { "Download origin mismatch" }
                    update = result
                    ui {
                        findViewById<View>(R.id.update_panel).visibility = View.VISIBLE
                        findViewById<TextView>(R.id.latest).text = "${result.getString("version_name")} · $version"
                        findViewById<TextView>(R.id.notes).text = result.getString("release_notes")
                        findViewById<TextView>(R.id.metadata).text = String.format(Locale.US, "%.1f MB · Mandatory: %s", size / 1048576.0, result.getBoolean("mandatory"))
                        findViewById<Button>(R.id.later).visibility = if (result.getBoolean("mandatory")) View.GONE else View.VISIBLE
                        download.isEnabled = true
                        status.text = "Update available"
                    }
                } else { ui { status.text = "Up to date" } }
                try { heartbeat(base) } catch (_: Exception) { }
            } catch (e: Exception) { ui { status.text = e.message ?: "Check failed" } }
            finally { conn.disconnect(); ui { check.isEnabled = true; server.isEnabled = true } }
        }
    }

    private fun downloadUpdate() {
        val release = update ?: return
        download.isEnabled = false
        check.isEnabled = false
        server.isEnabled = false
        install.visibility = View.GONE
        verifiedFile = null
        progress.progress = 0
        progress.visibility = View.VISIBLE
        status.text = "Downloading…"
        executor.execute {
            val directory = File(cacheDir, "updates").apply { mkdirs() }
            val partial = File(directory, "incoming.apk")
            val conn = connection(release.getString("download_url"))
            try {
                conn.setRequestProperty("Accept", "application/vnd.android.package-archive")
                require(conn.responseCode == 200) { "Download returned HTTP ${conn.responseCode}" }
                val expectedSize = release.getLong("size")
                val hash = MessageDigest.getInstance("SHA-256")
                var total = 0L
                var lastProgress = 0L
                conn.inputStream.use { input -> partial.outputStream().use { output ->
                    val buffer = ByteArray(32_768)
                    while (true) {
                        if (Thread.currentThread().isInterrupted) throw InterruptedException()
                        val count = input.read(buffer)
                        if (count < 0) break
                        total += count
                        require(total <= expectedSize) { "APK exceeds declared size" }
                        output.write(buffer, 0, count)
                        hash.update(buffer, 0, count)
                        val now = System.currentTimeMillis()
                        if (now - lastProgress >= 100) {
                            lastProgress = now
                            val percent = (total * 100 / expectedSize).toInt()
                            ui { progress.progress = percent; status.text = "Downloading… $percent%" }
                        }
                    }
                } }
                val digest = hash.digest().joinToString("") { "%02x".format(it.toInt() and 0xff) }
                require(total == expectedSize && digest.equals(release.getString("sha256"), ignoreCase = true)) { "SHA256 verification failed" }
                val apkInfo = packageManager.getPackageArchiveInfo(partial.absolutePath, 0)
                require(apkInfo != null && apkInfo.packageName == packageName && apkInfo.versionCode.toLong() == release.getLong("version_code")) { "APK package or version mismatch" }
                val file = File(directory, "verified.apk")
                if (file.exists()) require(file.delete()) { "Cannot replace cached APK" }
                require(partial.renameTo(file)) { "Cannot save APK" }
                verifiedFile = file
                ui { progress.progress = 100; status.text = "Verification Passed"; install.visibility = View.VISIBLE }
            } catch (e: Exception) { partial.delete(); ui { status.text = e.message ?: "Download failed" } }
            finally { conn.disconnect(); ui { download.isEnabled = true; check.isEnabled = true; server.isEnabled = true } }
        }
    }

    private fun installUpdate() {
        val file = verifiedFile ?: return
        if (!file.exists()) { status.text = "Download the update again"; install.visibility = View.GONE; return }
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O && !packageManager.canRequestPackageInstalls()) {
            AlertDialog.Builder(this).setMessage("Allow EasyUpdate Demo to install updates")
                .setPositiveButton("Open Settings") { _, _ -> startActivity(Intent(Settings.ACTION_MANAGE_UNKNOWN_APP_SOURCES, Uri.parse("package:$packageName"))) }
                .setNegativeButton("Cancel", null).show()
            return
        }
        val uri = FileProvider.getUriForFile(this, "$packageName.files", file)
        try { startActivity(Intent(Intent.ACTION_VIEW).setDataAndType(uri, "application/vnd.android.package-archive").addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)) }
        catch (_: Exception) { status.text = "Package installer unavailable" }
    }

    override fun onDestroy() { executor.shutdownNow(); super.onDestroy() }
}
