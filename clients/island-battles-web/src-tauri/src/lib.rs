//! The desktop shell for the Island Battles web client. It holds no game code:
//! the window shows the web client's build, and API and localhost bot requests
//! go through the HTTP plugin, which sends them from Rust (no CORS) and only to
//! the hosts in `capabilities/`.

pub fn run() {
    tauri::Builder::default()
        .plugin(tauri_plugin_http::init())
        .run(tauri::generate_context!())
        .expect("error while running the Island Battles desktop app");
}
