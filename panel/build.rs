//! Compiles the operator API's .proto files without protoc: `protox`, in
//! pure Rust, parses them into a descriptor set that tonic's generator turns
//! into the client and the server stubs (the server ones serve the tests).

use std::path::PathBuf;

fn main() -> Result<(), Box<dyn std::error::Error>> {
    let proto_root = PathBuf::from("../api/proto");
    let package = proto_root.join("axlrbots/operator/v1");
    println!("cargo:rerun-if-changed={}", proto_root.display());
    let mut files: Vec<PathBuf> = std::fs::read_dir(&package)?
        .filter_map(Result::ok)
        .map(|entry| entry.path())
        .filter(|path| path.extension().is_some_and(|ext| ext == "proto"))
        .collect();
    files.sort();
    let descriptors = protox::compile(&files, [&proto_root])?;
    tonic_prost_build::configure()
        .build_server(true)
        .build_client(true)
        .compile_fds(descriptors)?;
    Ok(())
}
