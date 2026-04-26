package io.teacat.frpdeck

import android.app.Application
import io.teacat.frpdeck.core.FrpDeckEngine

/**
 * Application singleton. Holds the lazily-initialised FrpDeckEngine so
 * the foreground service and the Activity share the same gomobile
 * runtime regardless of which arrives first.
 *
 * The engine is NOT started here — Application.onCreate runs even for
 * trivial system events (BOOT_COMPLETED broadcasts on some OEMs), so we
 * defer Start() to either the user pressing the toggle in MainActivity
 * or the foreground service onStartCommand.
 */
class FrpDeckApp : Application() {

    val engine: FrpDeckEngine by lazy { FrpDeckEngine(this) }

    override fun onCreate() {
        super.onCreate()
        instance = this
    }

    companion object {
        @Volatile
        lateinit var instance: FrpDeckApp
            private set
    }
}
