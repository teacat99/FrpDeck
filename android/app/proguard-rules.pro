# R8 / ProGuard keep rules for FrpDeck.
#
# gomobile generates the `frpdeckmobile.*` and `go.*` packages with JNI
# entry points. Stripping or renaming these classes breaks the native
# bridge at runtime. We keep them wholesale because the surface is small.

-keep class frpdeckmobile.** { *; }
-keep class go.** { *; }
-keepclassmembers class * implements frpdeckmobile.LogHandler {
    void onLog(java.lang.String);
}

# Retrofit + Moshi keep rules — Moshi reflects on @JsonClass-annotated
# data classes; Retrofit reflects on Call return types.
-keepattributes Signature, InnerClasses, EnclosingMethod
-keepattributes RuntimeVisibleAnnotations, AnnotationDefault
-keep,allowobfuscation,allowshrinking interface retrofit2.Call
-keep,allowobfuscation,allowshrinking class kotlin.coroutines.Continuation
