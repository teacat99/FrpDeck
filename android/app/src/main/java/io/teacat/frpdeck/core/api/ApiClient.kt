package io.teacat.frpdeck.core.api

import com.squareup.moshi.Moshi
import com.squareup.moshi.kotlin.reflect.KotlinJsonAdapterFactory
import okhttp3.Interceptor
import okhttp3.OkHttpClient
import okhttp3.logging.HttpLoggingInterceptor
import retrofit2.Retrofit
import retrofit2.converter.moshi.MoshiConverterFactory
import java.util.concurrent.TimeUnit

/**
 * Retrofit factory bound to a particular base URL + bearer token. The
 * Engine creates a fresh ApiClient every time the loopback port or
 * admin token changes, so consumers can hold the reference for the
 * lifetime of a screen without worrying about staleness.
 */
class ApiClient private constructor(
    val service: FrpDeckService,
) {
    companion object {
        fun create(baseUrl: String, bearer: String): ApiClient {
            val auth = Interceptor { chain ->
                val req = chain.request().newBuilder()
                    .addHeader("Accept", "application/json")
                    .apply {
                        if (bearer.isNotEmpty()) {
                            addHeader("Authorization", "Bearer $bearer")
                        }
                    }
                    .build()
                chain.proceed(req)
            }
            val log = HttpLoggingInterceptor().apply {
                level = HttpLoggingInterceptor.Level.BASIC
            }
            val client = OkHttpClient.Builder()
                .addInterceptor(auth)
                .addInterceptor(log)
                .connectTimeout(5, TimeUnit.SECONDS)
                .readTimeout(15, TimeUnit.SECONDS)
                .writeTimeout(15, TimeUnit.SECONDS)
                .build()

            val moshi = Moshi.Builder()
                .add(KotlinJsonAdapterFactory())
                .build()

            val retrofit = Retrofit.Builder()
                .baseUrl(baseUrl)
                .client(client)
                .addConverterFactory(MoshiConverterFactory.create(moshi))
                .build()

            return ApiClient(retrofit.create(FrpDeckService::class.java))
        }
    }
}
