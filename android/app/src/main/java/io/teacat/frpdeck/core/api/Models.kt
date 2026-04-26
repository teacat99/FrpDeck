package io.teacat.frpdeck.core.api

import com.squareup.moshi.JsonClass

/**
 * Subset of the FrpDeck REST shapes that the Native Compose UI cares
 * about (Status / Endpoints / Tunnels tabs). The "More" tab uses the
 * embedded WebView and does not need typed clients.
 *
 * Only fields used in v0.1 mobile are wired. Adding a field is a
 * one-line change here + the corresponding Composable, no protobuf
 * regeneration step.
 */
@JsonClass(generateAdapter = true)
data class Endpoint(
    val id: Long,
    val name: String,
    val group: String?,
    val addr: String,
    val port: Int,
    val protocol: String?,
    val enabled: Boolean,
    val auto_start: Boolean,
    val driver_mode: String?,
    val tunnel_count: Int?,
)

@JsonClass(generateAdapter = true)
data class Tunnel(
    val id: Long,
    val endpoint_id: Long,
    val name: String,
    val type: String,
    val role: String,
    val local_port: Int,
    val remote_port: Int?,
    val status: String,
    val enabled: Boolean,
    val expire_at: String?,
    val source: String?,
)

@JsonClass(generateAdapter = true)
data class TunnelListResponse(
    val items: List<Tunnel>,
    val total: Int,
)

@JsonClass(generateAdapter = true)
data class EndpointListResponse(
    val items: List<Endpoint>,
    val total: Int,
)

@JsonClass(generateAdapter = true)
data class StatusOverview(
    val endpoints_total: Int,
    val endpoints_enabled: Int,
    val tunnels_total: Int,
    val tunnels_running: Int,
    val frp_version: String,
)
