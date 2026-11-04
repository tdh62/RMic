pluginManagement {
    repositories {
        google()
        maven("https://maven.aliyun.com/repository/central")
        gradlePluginPortal()
    }
}
dependencyResolutionManagement {
    repositoriesMode.set(RepositoriesMode.FAIL_ON_PROJECT_REPOS)
    repositories {
        google()
        maven("https://maven.aliyun.com/repository/central")
        mavenCentral()
    }
}
rootProject.name = "RMicAndroid"
include(":app")
