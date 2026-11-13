use serde_json::{Map, Value};

use crate::application::PanelError;

/// The fields of one JSON object a definition carries opaquely, read one
/// by one: whatever is left when the mapper is done is a field the
/// contract does not know, which is refused rather than lost on the wire.
pub(super) struct JsonFields {
    scope: &'static str,
    fields: Map<String, Value>,
}

impl JsonFields {
    pub fn of(scope: &'static str, value: &Value) -> Result<Self, PanelError> {
        match value {
            Value::Object(fields) => Ok(Self {
                scope,
                fields: fields.clone(),
            }),
            _ => Err(PanelError::Invalid(format!("{scope} must be an object"))),
        }
    }

    pub fn take(&mut self, key: &str) -> Option<Value> {
        self.fields.remove(key).filter(|v| !v.is_null())
    }

    fn wrong(&self, key: &str, expected: &str) -> PanelError {
        PanelError::Invalid(format!("{}.{key} must be {expected}", self.scope))
    }

    pub fn string(&mut self, key: &str) -> Result<String, PanelError> {
        match self.take(key) {
            None => Ok(String::new()),
            Some(Value::String(s)) => Ok(s),
            Some(_) => Err(self.wrong(key, "text")),
        }
    }

    pub fn strings(&mut self, key: &str) -> Result<Vec<String>, PanelError> {
        match self.take(key) {
            None => Ok(Vec::new()),
            Some(Value::Array(items)) => items
                .into_iter()
                .map(|v| match v {
                    Value::String(s) => Ok(s),
                    _ => Err(self.wrong(key, "a list of text")),
                })
                .collect(),
            Some(_) => Err(self.wrong(key, "a list of text")),
        }
    }

    pub fn int(&mut self, key: &str) -> Result<i32, PanelError> {
        match self.take(key) {
            None => Ok(0),
            Some(v) => v
                .as_i64()
                .and_then(|n| i32::try_from(n).ok())
                .ok_or_else(|| self.wrong(key, "a whole number")),
        }
    }

    pub fn number(&mut self, key: &str) -> Result<f64, PanelError> {
        match self.take(key) {
            None => Ok(0.0),
            Some(v) => v.as_f64().ok_or_else(|| self.wrong(key, "a number")),
        }
    }

    pub fn optional_flag(&mut self, key: &str) -> Result<Option<bool>, PanelError> {
        match self.take(key) {
            None => Ok(None),
            Some(Value::Bool(b)) => Ok(Some(b)),
            Some(_) => Err(self.wrong(key, "true or false")),
        }
    }

    /// A nested object, as its own fields.
    pub fn object(&mut self, scope: &'static str, key: &str) -> Result<Option<Self>, PanelError> {
        match self.take(key) {
            None => Ok(None),
            Some(value) => Self::of(scope, &value).map(Some),
        }
    }

    /// A list of objects, each as its own fields.
    pub fn objects(&mut self, scope: &'static str, key: &str) -> Result<Vec<Self>, PanelError> {
        match self.take(key) {
            None => Ok(Vec::new()),
            Some(Value::Array(items)) => items.iter().map(|v| Self::of(scope, v)).collect(),
            Some(_) => Err(self.wrong(key, "a list")),
        }
    }

    /// A map of text to text.
    pub fn string_map(&mut self, key: &str) -> Result<std::collections::HashMap<String, String>, PanelError> {
        match self.take(key) {
            None => Ok(Default::default()),
            Some(Value::Object(map)) => map
                .into_iter()
                .map(|(k, v)| match v {
                    Value::String(s) => Ok((k, s)),
                    _ => Err(self.wrong(key, "a map of text")),
                })
                .collect(),
            Some(_) => Err(self.wrong(key, "a map of text")),
        }
    }

    /// Fails when a field was left unread.
    pub fn finish(self) -> Result<(), PanelError> {
        match self.fields.keys().next() {
            None => Ok(()),
            Some(key) => Err(PanelError::Invalid(format!(
                "{}.{key} is not a field the operator API knows",
                self.scope
            ))),
        }
    }
}
