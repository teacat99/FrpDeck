package io.teacat.frpdeck.core.api

import retrofit2.http.GET
import retrofit2.http.POST
import retrofit2.http.Path

/**
 * Retrofit interface for the FrpDeck REST surface. We keep this narrow:
 * Only the read paths and start/stop are wired into the Native Compose
 * tabs. Heavy-write screens (create/update endpoints, profiles, audit)
 * live behind the WebView and reuse the existing Vue UI as-is.
 */
interface FrpDeckService {

    @GET("api/endpoints")
    suspend fun listEndpoints(): EndpointListResponse

    @GET("api/tunnels")
    suspend fun listTunnels(): TunnelListResponse

    @POST("api/tunnels/{id}/start")
    suspend fun startTunnel(@Path("id") id: Long)

    @POST("api/tunnels/{id}/stop")
    suspend fun stopTunnel(@Path("id") id: Long)
}
