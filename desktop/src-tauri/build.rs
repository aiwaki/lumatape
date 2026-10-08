fn main() {
    println!("cargo:rerun-if-env-changed=LUMATAPE_UPDATE_ENDPOINT");
    println!("cargo:rerun-if-env-changed=LUMATAPE_UPDATE_PUBLIC_KEY");
    tauri_build::build()
}
