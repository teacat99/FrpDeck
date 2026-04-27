package io.teacat.frpdeck.core

import android.content.Context
import android.util.Log
import frpdeckmobile.Frpdeckmobile
import frpdeckmobile.LogHandler
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharedFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asSharedFlow
import kotlinx.coroutines.flow.asStateFlow
import java.io.File

/**
 * Thin wrapper over the gomobile-generated [Frpdeckmobile] singleton.
 *
 * The engine exposes:
 *   - [running] flow so callers can subscribe to start/stop transitions
 *   - [logs] flow that fans out lines from the Go side (via LogHandler)
 *   - [listenAddr] the loopback `host:port` the embedded server binds
 *
 * Native UI was removed in P6′/P7′; the WebView shell talks directly to
 * `http://${listenAddr}/` so the engine no longer ships a Retrofit
 * client — the same Vue SPA used by the desktop / Docker builds runs
 * unchanged in the embedded WebView.
 *
 * All coroutine usage is deferred to consumers — this class stays purely
 * synchronous so it can be instantiated from Application.onCreate without
 * pulling a coroutine scope on the boot path.
 */
class FrpDeckEngine(private val context: Context) {

    private val tag = "FrpDeck/Engine"

    /** Default loopback port chosen so it does not collide with the
     *  desktop default (8080) or 9201-9499 reserved for remote-mgmt
     *  visitor bindings. */
    private val defaultListen = "127.0.0.1:18080"

    /** App-private data directory under /data/data/<pkg>/files/frpdeck.
     *  Resolved once at construction so the SAF backup contract can read
     *  it without round-tripping through the gomobile bridge. */
    private val _dataDir: String = File(context.filesDir, "frpdeck").apply { mkdirs() }.absolutePath
    fun dataDir(): String = _dataDir

    private val _running = MutableStateFlow(false)
    val running: StateFlow<Boolean> = _running.asStateFlow()

    private val _listenAddr = MutableStateFlow("")
    val listenAddr: StateFlow<String> = _listenAddr.asStateFlow()

    private val _logs = MutableSharedFlow<String>(replay = 256, extraBufferCapacity = 64)
    val logs: SharedFlow<String> = _logs.asSharedFlow()

    @Volatile
    private var logHandle: String = ""

    /**
     * Boot the embedded server. Idempotent — calling Start while running
     * is a no-op (matches the gomobile contract which would error).
     */
    @Synchronized
    fun start(): Result<Unit> {
        if (Frpdeckmobile.isRunning()) {
            _running.value = true
            _listenAddr.value = Frpdeckmobile.listenAddr()
            return Result.success(Unit)
        }
        return try {
            Frpdeckmobile.start(
                _dataDir,
                defaultListen,
                "admin",
                /* adminPassword = */ "",
                "frpdeck-android",
            )
            attachLogHandler()
            _listenAddr.value = Frpdeckmobile.listenAddr()
            _running.value = true
            Log.i(tag, "started on ${_listenAddr.value}")
            Result.success(Unit)
        } catch (t: Throwable) {
            Log.e(tag, "start failed", t)
            Result.failure(t)
        }
    }

    @Synchronized
    fun stop(): Result<Unit> {
        if (!Frpdeckmobile.isRunning()) {
            _running.value = false
            return Result.success(Unit)
        }
        return try {
            detachLogHandler()
            Frpdeckmobile.stop()
            _running.value = false
            _listenAddr.value = ""
            Log.i(tag, "stopped")
            Result.success(Unit)
        } catch (t: Throwable) {
            Log.e(tag, "stop failed", t)
            Result.failure(t)
        }
    }

    /** Mint a fresh 24h admin JWT. Returns "" when not running. */
    fun adminToken(): String = Frpdeckmobile.adminToken()

    fun version(): String = Frpdeckmobile.version()

    private fun attachLogHandler() {
        if (logHandle.isNotEmpty()) return
        val h = object : LogHandler {
            override fun onLog(line: String?) {
                if (line.isNullOrEmpty()) return
                _logs.tryEmit(line)
            }
        }
        logHandle = Frpdeckmobile.addLogHandler(h)
    }

    private fun detachLogHandler() {
        if (logHandle.isNotEmpty()) {
            Frpdeckmobile.removeLogHandler(logHandle)
            logHandle = ""
        }
    }
}
