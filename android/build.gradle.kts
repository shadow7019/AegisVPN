// AegisVPN Android — root build script.
// AGP 8.5.x + Kotlin 2.0.x. The Compose compiler is wired through the
// org.jetbrains.kotlin.plugin.compose plugin (required since Kotlin 2.0).
plugins {
    id("com.android.application") version "8.5.2" apply false
    id("org.jetbrains.kotlin.android") version "2.0.20" apply false
    id("org.jetbrains.kotlin.plugin.compose") version "2.0.20" apply false
}
