package navapp.components

import android.app.Application
import android.app.Service
import android.content.BroadcastReceiver
import android.content.ContentProvider
import android.content.ContentValues
import android.content.Context
import android.content.Intent
import android.database.Cursor
import android.net.Uri
import android.os.IBinder
import androidx.activity.ComponentActivity
import androidx.work.CoroutineWorker
import androidx.work.WorkerParameters

// Components: an Application, a Service, a BroadcastReceiver and a ContentProvider the manifest
// declares, and a worker by its base class (an abstract base is not one); each reports to Tracker.
// PushService takes a push (its intent filter): AlertActivity, which only its code opens, gets the
// push entry.

object Tracker {
    fun track(event: String) = println(event)
}

class NavApp : Application() {
    override fun onCreate() {
        super.onCreate()
        Tracker.track("start")
    }
}

class SyncService : Service() {
    override fun onBind(intent: Intent?): IBinder? = null

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        Tracker.track("sync")
        return START_NOT_STICKY
    }
}

class BootReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) = Tracker.track("boot")
}

class NotesProvider : ContentProvider() {
    override fun onCreate() = true

    override fun query(uri: Uri, projection: Array<String>?, selection: String?, selectionArgs: Array<String>?, sortOrder: String?): Cursor? {
        Tracker.track("query")
        return null
    }

    override fun getType(uri: Uri): String? = null
    override fun insert(uri: Uri, values: ContentValues?): Uri? = null
    override fun delete(uri: Uri, selection: String?, selectionArgs: Array<String>?) = 0
    override fun update(uri: Uri, values: ContentValues?, selection: String?, selectionArgs: Array<String>?) = 0
}

abstract class TrackedWorker(context: Context, params: WorkerParameters) : CoroutineWorker(context, params) {
    fun report(event: String) = Tracker.track(event)
}

class SyncWorker(context: Context, params: WorkerParameters) : TrackedWorker(context, params) {
    override suspend fun doWork(): Result {
        report("work")
        return Result.success()
    }
}

class PushService : Service() {
    override fun onBind(intent: Intent?): IBinder? = null

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        Alerts.show(this)
        return START_NOT_STICKY
    }
}

object Alerts {
    fun show(context: Context) = context.startActivity(Intent(context, AlertActivity::class.java))
}

class AlertActivity : ComponentActivity()
