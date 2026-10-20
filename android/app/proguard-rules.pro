# AegisVPN Android proguard rules (release currently runs with minify disabled;
# rules kept here so enabling R8 later is safe).

# gomobile binding: the aegislib AAR (plus its go.Seq runtime) is reached from
# Go through JNI + reflection, so its classes must never be renamed/stripped.
-keep class aegislib.** { *; }
-keep class go.** { *; }

# gomobile-generated proxies live under <pkg>.<Pkg> with JNI name lookups.
-keepclassmembers class aegislib.** { *; }
