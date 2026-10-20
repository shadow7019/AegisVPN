// AegisVPN Android — Gradle settings.
// Repositories are centralized here (FAIL_ON_PROJECT_REPOS) so modules cannot
// add their own; google() first for AndroidX/AGP artifacts.
pluginManagement {
    repositories {
        google()
        mavenCentral()
        gradlePluginPortal()
    }
}

dependencyResolutionManagement {
    repositoriesMode.set(RepositoriesMode.FAIL_ON_PROJECT_REPOS)
    repositories {
        google()
        mavenCentral()
    }
}

rootProject.name = "AegisVPN"
include(":app")
