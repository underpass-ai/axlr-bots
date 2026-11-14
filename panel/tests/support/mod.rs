//! What the gRPC tests share: a local TLS server that plays the operator
//! API (a CA made for the test signs the server's certificate and the
//! adapter trusts that CA alone), and the helpers that answer one call.

#![allow(dead_code)]

use std::net::SocketAddr;
use std::sync::{Arc, Mutex};

use axlr_bots_panel::application::PanelConfig;
use axum::Router;
use axum::body::Body;
use axum::http::{HeaderMap, Request};
use axum::response::Response;
use axum_server::Handle;
use axum_server::tls_rustls::RustlsConfig;
use rcgen::{BasicConstraints, CertificateParams, IsCa, Issuer, KeyPair};
use rustls::pki_types::{CertificateDer, PrivateKeyDer, PrivatePkcs8KeyDer};
use tonic::{Status, server::Grpc};
use tonic_prost::ProstCodec;

pub const TOKEN: &str = "operator-token-for-tests";

/// What a call carried: the method, the credentials and the message.
#[derive(Debug, Clone)]
pub struct Recorded {
    pub path: String,
    pub authorization: String,
    pub actor: String,
    pub message: String,
}

#[derive(Clone, Default)]
pub struct Recorder {
    pub calls: Arc<Mutex<Vec<Recorded>>>,
}

impl Recorder {
    pub fn record(&self, path: &str, headers: &HeaderMap, message: String) {
        let header = |name: &str| {
            headers
                .get(name)
                .and_then(|v| v.to_str().ok())
                .unwrap_or_default()
                .to_string()
        };
        self.calls.lock().unwrap().push(Recorded {
            path: path.rsplit('/').next().unwrap_or_default().to_string(),
            authorization: header("authorization"),
            actor: header("x-axlr-actor"),
            message,
        });
    }
}

pub struct TestServer {
    pub address: SocketAddr,
    pub recorder: Recorder,
    pub dir: tempfile::TempDir,
    handle: Handle,
}

impl TestServer {
    pub fn config(&self) -> PanelConfig {
        PanelConfig {
            api_url: format!("https://127.0.0.1:{}/", self.address.port()),
            ca_path: self.dir.path().join("ca.crt"),
            token_file: self.dir.path().join("api.token"),
            actor: None,
            cert_path: Some(self.dir.path().join("operator.crt")),
            key_path: Some(self.dir.path().join("operator.key")),
        }
    }

    pub fn calls(&self) -> Vec<Recorded> {
        self.recorder.calls.lock().unwrap().clone()
    }

    pub fn last(&self) -> Recorded {
        self.calls().last().cloned().unwrap()
    }
}

impl Drop for TestServer {
    fn drop(&mut self) {
        self.handle.shutdown();
    }
}

/// Writes a client certificate signed by `issuer` as `<name>.crt` and
/// `<name>.key`; `operator` gives it the marks of an operator's (the
/// organisation and the `axlr-bots:operator:<name>` URI).
fn write_client(dir: &std::path::Path, issuer: &Issuer<'_, &KeyPair>, name: &str, operator: bool) {
    let key = KeyPair::generate().unwrap();
    let mut params = CertificateParams::new(Vec::<String>::new()).unwrap();
    params.distinguished_name.push(rcgen::DnType::CommonName, "alice");
    params.extended_key_usages = vec![rcgen::ExtendedKeyUsagePurpose::ClientAuth];
    if operator {
        params
            .distinguished_name
            .push(rcgen::DnType::OrganizationName, "axlr-bots operators");
        params.subject_alt_names = vec![rcgen::SanType::URI("axlr-bots:operator:alice".try_into().unwrap())];
    }
    let cert = params.signed_by(&key, issuer).unwrap();
    std::fs::write(dir.join(format!("{name}.crt")), cert.pem()).unwrap();
    std::fs::write(dir.join(format!("{name}.key")), key.serialize_pem()).unwrap();
}

/// A fleet CA, a server certificate for 127.0.0.1 it signs, the CA and the
/// token written where the panel reads them, an operator certificate of
/// that CA (`operator.crt`, `operator.key`) and one of another CA
/// (`stranger.crt`, `stranger.key`), and `app` serving over mutual TLS:
/// a client certificate that chains to the CA is required.
pub async fn serve(app: Router, recorder: Recorder) -> TestServer {
    let ca_key = KeyPair::generate().unwrap();
    let mut ca_params = CertificateParams::new(Vec::<String>::new()).unwrap();
    ca_params.is_ca = IsCa::Ca(BasicConstraints::Unconstrained);
    ca_params
        .distinguished_name
        .push(rcgen::DnType::CommonName, "test fleet CA");
    let ca_cert = ca_params.self_signed(&ca_key).unwrap();
    let issuer = Issuer::new(ca_params, &ca_key);
    let server_key = KeyPair::generate().unwrap();
    let server_params = CertificateParams::new(vec!["127.0.0.1".to_string(), "localhost".to_string()]).unwrap();
    let server_cert = server_params.signed_by(&server_key, &issuer).unwrap();

    let dir = tempfile::tempdir().unwrap();
    write_client(dir.path(), &issuer, "operator", true);
    let other_key = KeyPair::generate().unwrap();
    let mut other_params = CertificateParams::new(Vec::<String>::new()).unwrap();
    other_params.is_ca = IsCa::Ca(BasicConstraints::Unconstrained);
    let other_issuer = Issuer::new(other_params, &other_key);
    write_client(dir.path(), &other_issuer, "stranger", true);
    std::fs::write(dir.path().join("ca.crt"), ca_cert.pem()).unwrap();
    std::fs::write(dir.path().join("api.token"), format!("{TOKEN}\n")).unwrap();

    let provider = Arc::new(rustls::crypto::ring::default_provider());
    let mut roots = rustls::RootCertStore::empty();
    roots.add(CertificateDer::from(ca_cert.der().to_vec())).unwrap();
    let verifier = rustls::server::WebPkiClientVerifier::builder_with_provider(Arc::new(roots), provider.clone())
        .build()
        .unwrap();
    let mut tls = rustls::ServerConfig::builder_with_provider(provider)
        .with_safe_default_protocol_versions()
        .unwrap()
        .with_client_cert_verifier(verifier)
        .with_single_cert(
            vec![CertificateDer::from(server_cert.der().to_vec())],
            PrivateKeyDer::Pkcs8(PrivatePkcs8KeyDer::from(server_key.serialize_der())),
        )
        .unwrap();
    tls.alpn_protocols = vec![b"h2".to_vec(), b"http/1.1".to_vec()];
    let handle = Handle::new();
    let server = axum_server::bind_rustls("127.0.0.1:0".parse().unwrap(), RustlsConfig::from_config(Arc::new(tls)))
        .handle(handle.clone());
    tokio::spawn(server.serve(app.into_make_service()));
    let address = handle.listening().await.unwrap();
    TestServer {
        address,
        recorder,
        dir,
        handle,
    }
}

/// One unary call answered by `answer`.
pub async fn unary<Req, Resp>(
    request: Request<Body>,
    answer: impl Fn(Req) -> Result<Resp, Status> + Send + 'static,
) -> Response
where
    Req: prost::Message + Default + Send + Sync + 'static,
    Resp: prost::Message + Send + Sync + 'static,
{
    struct Answer<F>(F);
    impl<Req, Resp, F> tonic::server::UnaryService<Req> for Answer<F>
    where
        F: Fn(Req) -> Result<Resp, Status>,
        Resp: Send + 'static,
    {
        type Response = Resp;
        type Future = std::future::Ready<Result<tonic::Response<Resp>, Status>>;

        fn call(&mut self, request: tonic::Request<Req>) -> Self::Future {
            std::future::ready((self.0)(request.into_inner()).map(tonic::Response::new))
        }
    }
    let mut grpc = Grpc::new(ProstCodec::<Resp, Req>::default());
    grpc.unary(Answer(answer), request).await.map(Body::new)
}

/// One server-streaming call: `messages`, then silence until the client
/// goes away (`then_quiet`) or the end of the stream.
pub async fn streaming<Req, Resp>(
    request: Request<Body>,
    messages: Vec<Result<Resp, Status>>,
    then_quiet: bool,
) -> Response
where
    Req: prost::Message + Default + Send + Sync + 'static,
    Resp: prost::Message + Clone + Send + Sync + 'static,
{
    struct Stream<Resp>(Vec<Result<Resp, Status>>, bool);
    impl<Req, Resp> tonic::server::ServerStreamingService<Req> for Stream<Resp>
    where
        Resp: prost::Message + Clone + Send + Sync + 'static,
    {
        type Response = Resp;
        type ResponseStream = futures::stream::BoxStream<'static, Result<Resp, Status>>;
        type Future = std::future::Ready<Result<tonic::Response<Self::ResponseStream>, Status>>;

        fn call(&mut self, _request: tonic::Request<Req>) -> Self::Future {
            use futures::StreamExt;
            let items = futures::stream::iter(self.0.clone());
            let stream: Self::ResponseStream = if self.1 {
                items.chain(futures::stream::pending()).boxed()
            } else {
                items.boxed()
            };
            std::future::ready(Ok(tonic::Response::new(stream)))
        }
    }
    let mut grpc = Grpc::new(ProstCodec::<Resp, Req>::default());
    grpc.server_streaming(Stream(messages, then_quiet), request)
        .await
        .map(Body::new)
}

/// The status a failed call answers with, as an HTTP response.
pub fn refused(status: Status) -> Response {
    status.into_http::<Body>().map(Body::new)
}
