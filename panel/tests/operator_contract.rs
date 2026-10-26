//! The panel against the operator API's contract,
//! `docs/reference/operator-api.json`, which the control plane's own tests
//! hold to the server (`adapters/unixapi/contract_test.go`).
//!
//! Every answer the panel reads is built from its schema three ways (every
//! field, only the required ones, every enum a value from a newer control
//! plane) and must read without an error: a status or a trigger the panel
//! does not know yet is shown as unknown, never as "Bad answer".

use std::path::Path;

use axlr_bots_panel::domain::{
    AutonomyLevel, Bot, BotStatus, CancelledTask, DecidedTask, EnrollmentToken, Event, Execution, ExecutionKind,
    ExecutionOutcome, Node, NodeStatus, Overview, RevokedNode, SubmittedTask, Task, TaskStatus, TriggerType,
};
use serde::de::DeserializeOwned;
use serde_json::{Map, Value, json};

const FUTURE: &str = "FromANewerControlPlane";

fn contract() -> Value {
    let path = Path::new(env!("CARGO_MANIFEST_DIR")).join("../docs/reference/operator-api.json");
    let text = std::fs::read_to_string(&path).unwrap_or_else(|e| panic!("{}: {e}", path.display()));
    serde_json::from_str(&text).expect("the contract is JSON")
}

fn schema<'a>(spec: &'a Value, name: &str) -> &'a Value {
    spec.pointer(&format!("/components/schemas/{name}"))
        .unwrap_or_else(|| panic!("no schema {name} in the contract"))
}

fn resolve<'a>(spec: &'a Value, node: &'a Value) -> &'a Value {
    match node.get("$ref").and_then(Value::as_str) {
        Some(reference) => resolve(
            spec,
            schema(spec, reference.trim_start_matches("#/components/schemas/")),
        ),
        None => node,
    }
}

#[derive(Clone, Copy, PartialEq)]
enum Shape {
    /// Every property, and one the contract does not name yet.
    Full,
    /// Only the required properties.
    Minimal,
    /// Every property, each enum a value no version of the panel knows.
    Future,
}

fn sample(spec: &Value, node: &Value, shape: Shape) -> Value {
    let node = resolve(spec, node);
    if let Some(values) = node.get("enum").and_then(Value::as_array) {
        return if shape == Shape::Future {
            json!(FUTURE)
        } else {
            values[0].clone()
        };
    }
    if let Some(example) = node.get("example") {
        return example.clone();
    }
    match node.get("type").and_then(Value::as_str) {
        Some("string") if node.get("format").and_then(Value::as_str) == Some("date-time") => {
            json!("2026-10-26T12:00:00Z")
        }
        Some("string") => json!("sample"),
        Some("integer") => json!(1),
        Some("number") => json!(0.5),
        Some("boolean") => json!(true),
        Some("array") => match shape {
            Shape::Minimal => json!([]),
            _ => json!([sample(spec, &node["items"], shape)]),
        },
        Some("object") => object(spec, node, shape),
        _ => json!({}),
    }
}

fn object(spec: &Value, node: &Value, shape: Shape) -> Value {
    let mut out = Map::new();
    if let Some(properties) = node.get("properties").and_then(Value::as_object) {
        let required: Vec<&str> = node
            .get("required")
            .and_then(Value::as_array)
            .map(|r| r.iter().filter_map(Value::as_str).collect())
            .unwrap_or_default();
        for (name, property) in properties {
            if shape == Shape::Minimal && !required.contains(&name.as_str()) {
                continue;
            }
            out.insert(name.clone(), sample(spec, property, shape));
        }
        if shape != Shape::Minimal {
            out.insert("fieldFromANewerControlPlane".into(), json!({"any": "thing"}));
        }
    } else if let Some(values) = node.get("additionalProperties") {
        let key = match node.get("propertyNames") {
            Some(names) => sample(spec, names, shape).as_str().unwrap_or("key").to_string(),
            None => "key".to_string(),
        };
        if shape != Shape::Minimal {
            out.insert(key, sample(spec, values, shape));
        }
    }
    Value::Object(out)
}

fn reads<T: DeserializeOwned>(spec: &Value, name: &str) {
    for shape in [Shape::Full, Shape::Minimal, Shape::Future] {
        let value = sample(spec, schema(spec, name), shape);
        if let Err(error) = serde_json::from_value::<T>(value.clone()) {
            panic!(
                "the panel cannot read a {name} ({}): {error}\n{value:#}",
                match shape {
                    Shape::Full => "every field",
                    Shape::Minimal => "required fields only",
                    Shape::Future => "enums from a newer control plane",
                }
            );
        }
    }
}

#[test]
fn reads_every_answer_it_asks_for() {
    let spec = contract();
    reads::<Overview>(&spec, "Overview");
    reads::<Node>(&spec, "NodeSnapshot");
    reads::<EnrollmentToken>(&spec, "EnrollTokenResponse");
    reads::<RevokedNode>(&spec, "RevokeResponse");
    reads::<Bot>(&spec, "BotSnapshot");
    reads::<Event>(&spec, "Event");
    reads::<Task>(&spec, "TaskSnapshot");
    reads::<SubmittedTask>(&spec, "SubmitResponse");
    reads::<CancelledTask>(&spec, "CancelResponse");
    reads::<DecidedTask>(&spec, "RunResponse");
    reads::<Execution>(&spec, "ExecutionSnapshot");
}

fn values(spec: &Value, name: &str) -> Vec<String> {
    schema(spec, name)["enum"]
        .as_array()
        .unwrap_or_else(|| panic!("{name} has no enum"))
        .iter()
        .map(|v| v.as_str().unwrap().to_string())
        .collect()
}

fn knows<T: DeserializeOwned + PartialEq + std::fmt::Debug>(spec: &Value, name: &str, unknown: T) {
    for value in values(spec, name) {
        let read: T = serde_json::from_value(json!(value)).unwrap_or_else(|e| panic!("{name} {value:?}: {e}"));
        assert_ne!(
            read, unknown,
            "{name} {value:?} is in the contract and the panel reads it as unknown"
        );
    }
    let future: T = serde_json::from_value(json!(FUTURE)).unwrap_or_else(|e| panic!("{name} {FUTURE:?}: {e}"));
    assert_eq!(
        future, unknown,
        "{name}: a value the panel does not know reads as unknown"
    );
}

#[test]
fn knows_every_value_the_contract_lists_and_reads_a_new_one_as_unknown() {
    let spec = contract();
    knows(&spec, "TaskStatus", TaskStatus::Unknown);
    knows(&spec, "BotStatus", BotStatus::Unknown);
    knows(&spec, "NodeStatus", NodeStatus::Unknown);
    knows(&spec, "ExecutionOutcome", ExecutionOutcome::Unknown);
    knows(&spec, "ExecutionKind", ExecutionKind::Unknown);
    knows(&spec, "TriggerType", TriggerType::Unknown);
    knows(&spec, "AutonomyLevel", AutonomyLevel::Unknown);
}

#[test]
fn filters_by_every_task_status_the_contract_lists() {
    let spec = contract();
    let listed = values(&spec, "TaskStatus");
    let offered: Vec<String> = TaskStatus::ALL.iter().map(|s| s.name().to_string()).collect();
    assert_eq!(offered, listed, "the Tasks filter offers what the control plane has");
}
