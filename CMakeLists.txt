cmake_minimum_required(VERSION 3.24)
project(lumatape_native VERSION 0.1.0 LANGUAGES C CXX)

if(NOT WIN32 OR NOT CMAKE_SIZEOF_VOID_P EQUAL 8)
  message(FATAL_ERROR "LumaTape production binaries target Windows x64. On macOS run Go tests and scripts/validate-shaders-macos.sh.")
endif()

option(LUMATAPE_BUILD_CAPTURE "Build optional Windows Graphics Capture bridge (MSVC required)" ON)
include(CTest)
include(FetchContent)
set(BUILD_SHARED_LIBS ON CACHE BOOL "" FORCE)
set(GLFW_BUILD_EXAMPLES OFF CACHE BOOL "" FORCE)
set(GLFW_BUILD_TESTS OFF CACHE BOOL "" FORCE)
set(GLFW_BUILD_DOCS OFF CACHE BOOL "" FORCE)
set(GLFW_INSTALL OFF CACHE BOOL "" FORCE)
# GLFW 3.4 release: immutable upstream commit, not a moving branch.
FetchContent_Declare(glfw GIT_REPOSITORY https://github.com/glfw/glfw.git
  GIT_TAG a74efa0d5628b74adc0426af4c5710e287fa7c2c GIT_SHALLOW FALSE)
FetchContent_MakeAvailable(glfw)
if(MSVC)
  set_property(TARGET glfw PROPERTY MSVC_RUNTIME_LIBRARY "MultiThreaded$<$<CONFIG:Debug>:Debug>")
endif()
install(TARGETS glfw RUNTIME DESTINATION .)
install(FILES "${glfw_SOURCE_DIR}/LICENSE.md" DESTINATION licenses RENAME GLFW-LICENSE.md)
install(FILES LICENSE third_party/Go-LICENSE.txt
  third_party/cppwinrt-LICENSE.txt third_party/MSVC-STL-LICENSE.txt DESTINATION licenses)
if(LUMATAPE_BUILD_CAPTURE)
  add_subdirectory(native/capture)
endif()
