package com.zlight106.easyupdate.demo

import android.app.Activity
import android.app.Instrumentation
import android.content.Intent
import android.os.Bundle
import android.os.Build
import android.widget.Button
import android.widget.EditText
import android.widget.TextView

// No test framework dependency: run with `adb shell am instrument -w -e server URL …`.
class SmokeInstrumentation : Instrumentation() {
    private var options = Bundle()
    override fun onCreate(arguments: Bundle?) { super.onCreate(arguments); options = arguments ?: Bundle(); start() }
    override fun onStart() {
        val report = Bundle()
        try {
            val activity = startActivitySync(Intent(targetContext, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
            runOnMainSync {
                activity.findViewById<EditText>(R.id.server_url).setText(options.getString("server", "http://10.0.2.2:8081"))
                activity.findViewById<Button>(R.id.check).performClick()
            }
            await(activity, "Update available")
            runOnMainSync { activity.findViewById<Button>(R.id.download).performClick() }
            await(activity, "Verification Passed")
            runOnMainSync {
                require(Build.VERSION.SDK_INT < Build.VERSION_CODES.O || activity.packageManager.canRequestPackageInstalls()) { "Check / Download / SHA256 passed; grant installation permission before installer test" }
                activity.findViewById<Button>(R.id.install).performClick()
                require(activity.findViewById<TextView>(R.id.status).text.toString() != "Package installer unavailable") { "Package installer unavailable" }
            }
            report.putString("stream", "PASS Check / Download / SHA256 / FileProvider installer intent\n")
            finish(Activity.RESULT_OK, report)
        } catch (error: Throwable) {
            report.putString("stream", "FAIL: ${error.message}\n")
            finish(Activity.RESULT_CANCELED, report)
        }
    }
    private fun await(activity: Activity, expected: String) {
        val deadline = System.currentTimeMillis() + 60_000
        var text = ""
        while (System.currentTimeMillis() < deadline) {
            runOnMainSync { text = activity.findViewById<TextView>(R.id.status).text.toString() }
            if (text == expected) return
            Thread.sleep(100)
        }
        error("Expected '$expected', received '$text'")
    }
}
