package io.teacat.frpdeck.ui.screens

import android.annotation.SuppressLint
import android.webkit.WebSettings
import android.webkit.WebView
import android.webkit.WebViewClient
import androidx.activity.compose.BackHandler
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.viewinterop.AndroidView

/**
 * Compose wrapper around the AndroidView WebView. The token is injected
 * via evaluateJavascript("localStorage.setItem('pp_token', ...)") AFTER
 * the page finishes loading, then the page is reloaded so the SPA picks
 * up the token from localStorage on its next bootstrap. (The desktop UI
 * stores the access token under the key `pp_token` — see auth store.)
 */
@SuppressLint("SetJavaScriptEnabled")
@Composable
fun WebViewScreen(url: String, token: String, onBack: () -> Unit) {
    BackHandler(enabled = true) { onBack() }

    val webView = remember { mutableWebViewHolder() }

    Box(modifier = Modifier.fillMaxSize()) {
        AndroidView(
            modifier = Modifier.fillMaxSize(),
            factory = { ctx ->
                WebView(ctx).apply {
                    settings.javaScriptEnabled = true
                    settings.domStorageEnabled = true
                    settings.cacheMode = WebSettings.LOAD_DEFAULT
                    settings.mixedContentMode = WebSettings.MIXED_CONTENT_COMPATIBILITY_MODE
                    settings.useWideViewPort = true
                    settings.loadWithOverviewMode = true

                    webViewClient = object : WebViewClient() {
                        private var injected = false
                        override fun onPageFinished(view: WebView, finishedUrl: String) {
                            super.onPageFinished(view, finishedUrl)
                            if (injected) return
                            injected = true
                            if (token.isNotEmpty()) {
                                val js = """
                                    (function(){
                                      try {
                                        localStorage.setItem('pp_token', ${escapeForJs(token)});
                                        if (!location.search.includes('reloaded=1')) {
                                          var sep = location.search ? '&' : '?';
                                          location.replace(location.pathname + location.search + sep + 'reloaded=1');
                                        }
                                      } catch (e) { console.error(e); }
                                    })();
                                """.trimIndent()
                                view.evaluateJavascript(js, null)
                            }
                        }
                    }

                    webView.value = this
                    loadUrl(url)
                }
            },
            update = { view ->
                if (view.url != url) {
                    view.loadUrl(url)
                }
            },
        )
    }
}

private class WebViewHolder {
    var value: WebView? = null
}

private fun mutableWebViewHolder() = WebViewHolder()

private fun escapeForJs(s: String): String {
    val escaped = buildString {
        s.forEach { c ->
            when (c) {
                '\\' -> append("\\\\")
                '"'  -> append("\\\"")
                '\n' -> append("\\n")
                '\r' -> append("\\r")
                else -> append(c)
            }
        }
    }
    return "\"$escaped\""
}
