package io.teacat.frpdeck.backup

import android.content.Context
import android.net.Uri
import io.teacat.frpdeck.FrpDeckApp
import java.io.File
import java.io.IOException
import java.util.zip.ZipEntry
import java.util.zip.ZipInputStream
import java.util.zip.ZipOutputStream

/**
 * Hand-rolled, dependency-free zip writer/reader for the SAF backup
 * button on the More tab.
 *
 * The backup bundle is a plain ZIP containing:
 *   frpdeck.db          — SQLite DB (single-file)
 *   settings/           — anything the engine wrote into <dataDir>/settings/
 *   bin/                — bundled frpc binaries (small enough to round-trip)
 *
 * We deliberately do NOT export bin/ on by default in v0.1; it's huge and
 * users on a fresh device can re-download from the Profiles UI. The Zip
 * code is structured to add it later via [includeBinaries].
 */
object BackupBundle {

    /** Files that always travel with a backup, relative to dataDir. */
    private val ALWAYS_INCLUDE = listOf("frpdeck.db", "settings")

    /** Returns the number of bytes written (caller can surface to UI). */
    fun export(ctx: Context, target: Uri): Long {
        val app = ctx.applicationContext as FrpDeckApp
        val dataDir = File(app.engine.dataDir())

        var written = 0L
        val resolver = ctx.contentResolver
        val out = resolver.openOutputStream(target, "w")
            ?: throw IOException("backup: could not open ${target}")
        ZipOutputStream(out.buffered()).use { zip ->
            ALWAYS_INCLUDE.forEach { rel ->
                val src = File(dataDir, rel)
                if (!src.exists()) return@forEach
                written += addToZip(zip, src, prefix = rel)
            }
        }
        return written
    }

    /** Restore is destructive — the engine MUST be stopped before calling. */
    fun import(ctx: Context, source: Uri): Int {
        val app = ctx.applicationContext as FrpDeckApp
        val dataDir = File(app.engine.dataDir())
        if (!dataDir.exists()) dataDir.mkdirs()

        var entries = 0
        val resolver = ctx.contentResolver
        val input = resolver.openInputStream(source)
            ?: throw IOException("backup: could not open ${source}")
        ZipInputStream(input.buffered()).use { zip ->
            var entry: ZipEntry? = zip.nextEntry
            while (entry != null) {
                val rel = entry.name
                if (!rel.startsWith("frpdeck.db") && !rel.startsWith("settings")) {
                    zip.closeEntry()
                    entry = zip.nextEntry
                    continue
                }
                val target = File(dataDir, rel).apply {
                    parentFile?.mkdirs()
                }
                if (entry.isDirectory) {
                    target.mkdirs()
                } else {
                    target.outputStream().use { zip.copyTo(it) }
                    entries += 1
                }
                zip.closeEntry()
                entry = zip.nextEntry
            }
        }
        return entries
    }

    private fun addToZip(zip: ZipOutputStream, src: File, prefix: String): Long {
        var written = 0L
        if (src.isDirectory) {
            src.listFiles()?.forEach { child ->
                written += addToZip(zip, child, "$prefix/${child.name}")
            }
        } else {
            zip.putNextEntry(ZipEntry(prefix))
            src.inputStream().use { it.copyTo(zip) }
            zip.closeEntry()
            written += src.length()
        }
        return written
    }
}
