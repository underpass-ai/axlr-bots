use std::collections::BTreeMap;
use std::sync::Mutex;

use chrono::{DateTime, Duration, Utc};

use crate::application::{ControlPlane, PanelError, PortFuture};
use crate::domain::{
    Bot, BotDefinition, BotId, BotPatch, BotStatus, Budgets, CancelledTask, DecidedTask, Decision, EnrollmentRequest,
    EnrollmentToken, Event, Execution, ExecutionId, ExecutionKind, ExecutionOutcome, ExecutionResult, Node,
    NodeCapabilities, NodeId, NodeStatus, Overview, PendingDecision, Publication, Published, RevokedNode, Secret,
    SentTaskMessage, StoppedBotAnswer, SubmittedTask, Subscription, Task, TaskFilter, TaskId, TaskStatus,
    TaskSubmission, ToolCall, Topic, TopicListener, TriggerRef, Usage,
};

/// The control plane in memory: what the use cases and the interface are
/// tested against. It keeps the rules the real one keeps (an idempotent
/// submission, the bot and task transitions) and can be told to fail.
#[derive(Default)]
pub struct FakeControlPlane {
    state: Mutex<FakeState>,
}

#[derive(Default)]
struct FakeState {
    now: Option<DateTime<Utc>>,
    nodes: BTreeMap<NodeId, Node>,
    bots: BTreeMap<BotId, Bot>,
    tasks: Vec<Task>,
    executions: Vec<Execution>,
    events: Vec<Event>,
    failure: Option<PanelError>,
    /// Answers as a control plane from before the stop and message routes.
    older: bool,
    calls: Vec<String>,
    serial: u64,
}

impl FakeControlPlane {
    pub fn new() -> Self {
        Self::default()
    }

    /// Two nodes, three bots and a few tasks, one of them waiting for a
    /// person: the fleet of this machine, at `now`.
    pub fn sample(now: DateTime<Utc>) -> Self {
        let fake = Self::new();
        {
            let mut state = fake.state.lock().unwrap();
            state.now = Some(now);
            state.add_node(
                "gx10-ff95",
                "gx10-ff95",
                &[("host", "gx10-ff95")],
                NodeStatus::Online,
                now,
            );
            state.add_node(
                "wrx80",
                "wrx80-node1",
                &[("host", "wrx80-node1"), ("gpu", "rtx3090")],
                NodeStatus::Online,
                now,
            );
            state.add_bot("bot", crate::domain::AutonomyLevel::Observe, &[], now);
            state.add_bot(
                "bot-act-with-approval",
                crate::domain::AutonomyLevel::ActWithApproval,
                &[],
                now,
            );
            state.add_bot("bot-wrx80", crate::domain::AutonomyLevel::Observe, &["wrx80"], now);
            let done = state.add_task(
                "bot-wrx80",
                "List the files in your workspace",
                TaskStatus::Completed,
                now - Duration::minutes(30),
            );
            state.add_execution(
                &done,
                ExecutionOutcome::Completed,
                Some("wrx80"),
                "There are 3 files.",
                None,
                now - Duration::minutes(30),
            );
            let waiting = state.add_task(
                "bot-act-with-approval",
                "Write hello into hello.txt",
                TaskStatus::WaitingApproval,
                now - Duration::minutes(5),
            );
            let pending = PendingDecision {
                tool: "local_write".into(),
                call_id: "call-1".into(),
                session_id: "sess-1".into(),
                arguments: Some(serde_json::json!({"path": "hello.txt", "content": "hello"})),
            };
            state.add_execution(
                &waiting,
                ExecutionOutcome::DecisionRequired,
                Some("gx10-ff95"),
                "",
                Some(pending),
                now - Duration::minutes(5),
            );
            let failed = state.add_task(
                "bot",
                "Read the missing file",
                TaskStatus::Failed,
                now - Duration::minutes(2),
            );
            state.add_execution(
                &failed,
                ExecutionOutcome::Failed,
                Some("gx10-ff95"),
                "",
                None,
                now - Duration::minutes(2),
            );
            state.add_task(
                "bot",
                "Write the weekly report",
                TaskStatus::Expired,
                now - Duration::minutes(1),
            );
            state.add_task("bot", "Say the time", TaskStatus::Running, now - Duration::seconds(20));
        }
        fake
    }

    /// From now on every call fails with the error; `None` heals it.
    pub fn fail_with(&self, failure: Option<PanelError>) {
        self.state.lock().unwrap().failure = failure;
    }

    /// From now on it answers as a control plane from before the stop and
    /// message routes.
    pub fn without_newer_routes(&self) {
        self.state.lock().unwrap().older = true;
    }

    /// The calls made so far, as `route arg`.
    pub fn calls(&self) -> Vec<String> {
        self.state.lock().unwrap().calls.clone()
    }

    /// Sets a node's status, as a heartbeat or a silence would.
    pub fn set_node_status(&self, id: &str, status: NodeStatus) {
        let mut state = self.state.lock().unwrap();
        if let Some(node) = state.nodes.get_mut(&NodeId::new(id).unwrap()) {
            node.status = status;
        }
    }

    /// Puts a bot in teams, as a patch would, without an event.
    pub fn set_groups(&self, id: &str, groups: &[&str]) {
        let mut state = self.state.lock().unwrap();
        if let Some(bot) = state.bots.get_mut(&BotId::new(id).unwrap()) {
            bot.groups = groups.iter().map(|g| g.to_string()).collect();
        }
    }

    /// Sets the topics a bot listens to, as a patch or a bot's ask
    /// would, without an event.
    pub fn set_subscriptions(&self, id: &str, subscriptions: Vec<Subscription>) {
        let mut state = self.state.lock().unwrap();
        if let Some(bot) = state.bots.get_mut(&BotId::new(id).unwrap()) {
            bot.subscriptions = subscriptions;
        }
    }

    /// Records an event of a bot, as the control plane would publish it.
    pub fn publish(&self, kind: &str, bot: &str, payload: serde_json::Value) {
        let mut state = self.state.lock().unwrap();
        let payload = payload.as_object().cloned();
        state.bot_event_with(kind, &BotId::new(bot).unwrap(), payload);
    }

    /// What a running task's run says back to the person, as its node
    /// hands it over.
    pub fn say(&self, task: &TaskId, text: &str) {
        let mut state = self.state.lock().unwrap();
        let now = state.clock();
        let bot = state
            .tasks
            .iter()
            .find(|t| &t.id == task)
            .map(|t| t.bot_id.to_string())
            .unwrap();
        state.event("bot.task.said", &bot, task, now, serde_json::json!({ "text": text }));
    }

    /// A teammate's event heard as a message by a running task, as the
    /// control plane records it.
    pub fn tell(&self, task: &TaskId, text: &str, event: &str) {
        let mut state = self.state.lock().unwrap();
        let now = state.clock();
        let bot = state
            .tasks
            .iter()
            .find(|t| &t.id == task)
            .map(|t| t.bot_id.to_string())
            .unwrap();
        let from = format!("event:{event}");
        state.event(
            "bot.task.message",
            &bot,
            task,
            now,
            serde_json::json!({ "text": text, "from": from }),
        );
    }

    /// Sets what made a task: the event's fields and the notes it knew.
    pub fn set_input(&self, task: &TaskId, input: serde_json::Value) {
        let mut state = self.state.lock().unwrap();
        if let Some(t) = state.tasks.iter_mut().find(|t| &t.id == task) {
            t.input = Some(input);
        }
    }

    /// Finishes a task as a node's report would.
    pub fn finish_task(&self, id: &TaskId, status: TaskStatus) {
        let mut state = self.state.lock().unwrap();
        let now = state.clock();
        if let Some(task) = state.tasks.iter_mut().find(|t| &t.id == id) {
            task.status = status;
            task.updated_at = now;
        }
    }

    fn call<T: Send + 'static>(
        &self,
        route: String,
        act: impl FnOnce(&mut FakeState) -> Result<T, PanelError>,
    ) -> PortFuture<'_, T> {
        let result = {
            let mut state = self.state.lock().unwrap();
            state.calls.push(route);
            match state.failure.clone() {
                Some(failure) => Err(failure),
                None => act(&mut state),
            }
        };
        Box::pin(async move { result })
    }
}

impl FakeState {
    fn clock(&self) -> DateTime<Utc> {
        self.now.unwrap_or_else(Utc::now)
    }

    fn next(&mut self, prefix: &str) -> String {
        self.serial += 1;
        format!("{prefix}-{:04}", self.serial)
    }

    fn add_node(&mut self, id: &str, host: &str, labels: &[(&str, &str)], status: NodeStatus, now: DateTime<Utc>) {
        let node = Node {
            id: NodeId::new(id).unwrap(),
            host_name: host.into(),
            labels: labels.iter().map(|(k, v)| (k.to_string(), v.to_string())).collect(),
            capabilities: NodeCapabilities {
                axlr_version: "0.14.0".into(),
                policy_supported: true,
                correlation_supported: true,
                dedicated: false,
            },
            status,
            registered_at: now - Duration::days(1),
            last_seen_at: now - Duration::seconds(5),
            certificate_serial: Some(format!("{id}-serial")),
            certificate_not_after: Some(now + Duration::days(364)),
            revoked_at: None,
        };
        self.nodes.insert(node.id.clone(), node);
    }

    fn add_bot(&mut self, id: &str, level: crate::domain::AutonomyLevel, nodes: &[&str], now: DateTime<Utc>) {
        let bot = Bot {
            id: BotId::new(id).unwrap(),
            tenant_id: "default".into(),
            name: id.into(),
            owner: "operator".into(),
            mission: "Do what each task asks, within the policy.".into(),
            allowed_tools: vec!["local_read".into(), "local_list".into(), "local_write".into()],
            model: crate::domain::ModelPolicy {
                model: "anthropic/claude-haiku-5.5".into(),
                routing_policy: "cost-optimized".into(),
                allow_remote_providers: true,
                max_context_tokens: 32000,
            },
            budgets: Budgets {
                max_model_calls_per_task: 10,
                max_tool_calls_per_task: 20,
                max_delegation_depth: 0,
                task_timeout_seconds: 300,
            },
            level,
            tool_rules: Default::default(),
            triggers: vec![crate::domain::Trigger::manual()],
            placement: crate::domain::Placement {
                nodes: nodes.iter().map(|n| NodeId::new(n).unwrap()).collect(),
                ..Default::default()
            },
            groups: Vec::new(),
            subscriptions: Vec::new(),
            status: BotStatus::Enabled,
            created_at: now - Duration::days(1),
            updated_at: now - Duration::days(1),
        };
        self.bots.insert(bot.id.clone(), bot);
    }

    fn add_task(&mut self, bot: &str, objective: &str, status: TaskStatus, at: DateTime<Utc>) -> TaskId {
        let id = TaskId::new(&self.next("task")).unwrap();
        let key = self.next("key");
        self.tasks.push(Task {
            id: id.clone(),
            bot_id: BotId::new(bot).unwrap(),
            objective: objective.into(),
            status,
            idempotency_key: key,
            correlation_id: format!("trace-{}", id),
            trigger: TriggerRef {
                trigger_type: "manual".into(),
                name: "manual".into(),
            },
            input: None,
            execution_ids: Vec::new(),
            priority: String::new(),
            incident_id: String::new(),
            failure_reason: match status {
                TaskStatus::Failed => "the file does not exist".into(),
                TaskStatus::Expired => {
                    "approval expired: nobody decided the call call-2 (local_write) by its deadline".into()
                }
                _ => String::new(),
            },
            created_at: at,
            updated_at: at,
        });
        self.event("bot.task.submitted", bot, &id, at, serde_json::json!({}));
        id
    }

    fn add_execution(
        &mut self,
        task: &TaskId,
        outcome: ExecutionOutcome,
        node: Option<&str>,
        answer: &str,
        pending: Option<PendingDecision>,
        at: DateTime<Utc>,
    ) -> ExecutionId {
        let id = ExecutionId::new(&self.next("exec")).unwrap();
        let bot = self
            .tasks
            .iter()
            .find(|t| &t.id == task)
            .map(|t| t.bot_id.clone())
            .unwrap();
        let tool_calls = match &pending {
            Some(p) => vec![ToolCall {
                name: p.tool.clone(),
                call_id: p.call_id.clone(),
                decision: "ask".into(),
                status: "pending".into(),
            }],
            None => vec![ToolCall {
                name: "local_list".into(),
                call_id: "call-0".into(),
                decision: "auto_approve".into(),
                status: "completed".into(),
            }],
        };
        // A run that ended has the runtime's receipt, with each call's
        // timing, as a node reports it.
        let receipt = outcome.is_final().then(|| {
            let calls: Vec<serde_json::Value> = tool_calls
                .iter()
                .map(|c| {
                    serde_json::json!({
                        "call_id": c.call_id, "tool": c.name, "decision": c.decision, "status": c.status,
                        "started_at": (at + Duration::seconds(2)).to_rfc3339(), "duration_ms": 40,
                        "arguments_sha256": "00"
                    })
                })
                .collect();
            serde_json::json!({"schema": "axlr.execution-receipt/v1", "tool_calls": calls, "complete": true})
        });
        self.executions.push(Execution {
            receipt,
            id: id.clone(),
            task_id: task.clone(),
            bot_id: bot.clone(),
            started_at: at,
            ended_at: outcome.is_final().then(|| at + Duration::seconds(7)),
            outcome,
            result: ExecutionResult {
                answer: answer.into(),
                tool_calls,
                pending_decision: pending,
                ..Default::default()
            },
            usage: Usage {
                requests: 3,
                prompt_tokens: 12000,
                completion_tokens: 300,
                tool_calls: 1,
                cost_usd: 0.0012,
                ..Default::default()
            },
            failure_reason: if outcome == ExecutionOutcome::Failed {
                "the file does not exist".into()
            } else {
                String::new()
            },
            trace_path: String::new(),
            policy: None,
            correlation: Some(crate::domain::Correlation {
                bot_id: bot.to_string(),
                task_id: task.to_string(),
                execution_id: id.to_string(),
                trace_id: format!("trace-{task}"),
                carried: true,
                path: String::new(),
            }),
            kind: ExecutionKind::Run,
            decision: String::new(),
            node_id: node.map(|n| NodeId::new(n).unwrap()),
            lease_until: None,
        });
        if let Some(t) = self.tasks.iter_mut().find(|t| &t.id == task) {
            t.execution_ids.push(id.clone());
        }
        let kind = match outcome {
            ExecutionOutcome::Completed => "bot.task.completed",
            ExecutionOutcome::Failed => "bot.task.failed",
            ExecutionOutcome::DecisionRequired => "bot.task.waiting_approval",
            _ => "bot.task.started",
        };
        self.event(
            kind,
            bot.as_str(),
            task,
            at + Duration::seconds(7),
            serde_json::json!({"nodeId": node}),
        );
        id
    }

    fn event(&mut self, kind: &str, bot: &str, task: &TaskId, at: DateTime<Utc>, payload: serde_json::Value) {
        let id = self.next("evt");
        let serde_json::Value::Object(payload) = payload else {
            unreachable!()
        };
        self.events.push(Event {
            event_id: id,
            event_type: kind.into(),
            tenant_id: "default".into(),
            bot_id: bot.into(),
            node_id: String::new(),
            group_name: String::new(),
            task_id: task.to_string(),
            execution_id: String::new(),
            incident_id: String::new(),
            correlation_id: format!("trace-{task}"),
            created_at: at,
            payload: Some(payload),
        });
    }

    fn bot_event(&mut self, kind: &str, bot: &BotId) {
        self.bot_event_with(kind, bot, None);
    }

    fn bot_event_with(&mut self, kind: &str, bot: &BotId, payload: Option<serde_json::Map<String, serde_json::Value>>) {
        let id = self.next("evt");
        let now = self.clock();
        self.events.push(Event {
            event_id: id,
            event_type: kind.into(),
            tenant_id: "default".into(),
            bot_id: bot.to_string(),
            node_id: String::new(),
            group_name: String::new(),
            task_id: String::new(),
            execution_id: String::new(),
            incident_id: String::new(),
            correlation_id: String::new(),
            created_at: now,
            payload,
        });
    }

    fn task_mut(&mut self, id: &TaskId) -> Result<&mut Task, PanelError> {
        self.tasks
            .iter_mut()
            .find(|t| &t.id == id)
            .ok_or_else(|| PanelError::NotFound(format!("task {id} not found")))
    }

    fn bot(&self, id: &BotId) -> Result<&Bot, PanelError> {
        self.bots
            .get(id)
            .ok_or_else(|| PanelError::NotFound(format!("bot {id} not found")))
    }

    fn switch(&mut self, id: &BotId, to: BotStatus) -> Result<Bot, PanelError> {
        let now = self.clock();
        let bot = self
            .bots
            .get_mut(id)
            .ok_or_else(|| PanelError::NotFound(format!("bot {id} not found")))?;
        let allowed = match to {
            BotStatus::Enabled => bot.status.can_enable(),
            _ => bot.status.can_pause(),
        };
        if !allowed {
            return Err(PanelError::State(format!("bot {id} is {}", bot.status.name())));
        }
        bot.status = to;
        bot.updated_at = now;
        let bot = bot.clone();
        self.bot_event(
            if to == BotStatus::Enabled {
                "bot.enabled"
            } else {
                "bot.paused"
            },
            id,
        );
        Ok(bot)
    }

    fn overview(&self, since: DateTime<Utc>) -> Overview {
        let mut overview = Overview {
            since: Some(since),
            ..Default::default()
        };
        for status in NodeStatus::ALL {
            overview.nodes.insert(status.name().into(), 0);
        }
        for status in BotStatus::ALL {
            overview.bots.insert(status.name().into(), 0);
        }
        for status in TaskStatus::ALL {
            overview.tasks.insert(status.name().into(), 0);
            overview.tasks_since.insert(status.name().into(), 0);
        }
        for node in self.nodes.values() {
            *overview.nodes.entry(node.status.name().into()).or_default() += 1;
        }
        for bot in self.bots.values() {
            *overview.bots.entry(bot.status.name().into()).or_default() += 1;
        }
        for task in &self.tasks {
            *overview.tasks.entry(task.status.name().into()).or_default() += 1;
            if task.created_at >= since {
                *overview.tasks_since.entry(task.status.name().into()).or_default() += 1;
            }
        }
        for execution in self.executions.iter().filter(|e| e.started_at >= since) {
            overview.executions_since += 1;
            overview.cost_usd_since += execution.usage.cost_usd;
        }
        overview
    }
}

impl ControlPlane for FakeControlPlane {
    fn overview(&self, since: DateTime<Utc>) -> PortFuture<'_, Overview> {
        self.call("overview".into(), move |s| Ok(s.overview(since)))
    }

    fn list_nodes(&self) -> PortFuture<'_, Vec<Node>> {
        self.call("list_nodes".into(), |s| Ok(s.nodes.values().cloned().collect()))
    }

    fn get_node(&self, id: NodeId) -> PortFuture<'_, Node> {
        self.call(format!("get_node {id}"), move |s| {
            s.nodes
                .get(&id)
                .cloned()
                .ok_or_else(|| PanelError::NotFound(format!("node {id} not found")))
        })
    }

    fn mint_enrollment_token(&self, request: EnrollmentRequest) -> PortFuture<'_, EnrollmentToken> {
        self.call("mint_enrollment_token".into(), move |s| {
            let node_id = match request.node_id {
                Some(id) => id,
                None => NodeId::new(&s.next("node")).unwrap(),
            };
            let id = s.next("enr");
            Ok(EnrollmentToken {
                token: Secret::new(format!("axlr1.{id}.secret")),
                id,
                node_id,
                labels: request.labels,
                expires_at: s.clock() + Duration::hours(1),
            })
        })
    }

    fn revoke_node(&self, id: NodeId) -> PortFuture<'_, RevokedNode> {
        self.call(format!("revoke_node {id}"), move |s| {
            let now = s.clock();
            let node = s
                .nodes
                .get_mut(&id)
                .ok_or_else(|| PanelError::NotFound(format!("node {id} not found")))?;
            if !node.status.can_revoke() {
                return Err(PanelError::State(format!("node {id} is Revoked")));
            }
            node.status = NodeStatus::Revoked;
            node.revoked_at = Some(now);
            Ok(RevokedNode {
                node: node.clone(),
                failed_tasks: Vec::new(),
            })
        })
    }

    fn list_bots(&self) -> PortFuture<'_, Vec<Bot>> {
        self.call("list_bots".into(), |s| Ok(s.bots.values().cloned().collect()))
    }

    fn get_bot(&self, id: BotId) -> PortFuture<'_, Bot> {
        self.call(format!("get_bot {id}"), move |s| s.bot(&id).cloned())
    }

    fn register_bot(&self, definition: BotDefinition) -> PortFuture<'_, Bot> {
        self.call(format!("register_bot {}", definition.name), move |s| {
            let raw = if definition.id.is_empty() {
                definition.name.clone()
            } else {
                definition.id.clone()
            };
            let id = BotId::new(&raw)?;
            if s.bots.contains_key(&id) {
                return Err(PanelError::Conflict(format!("bot {id} exists")));
            }
            let now = s.clock();
            let bot = Bot {
                id: id.clone(),
                tenant_id: definition.tenant,
                name: definition.name,
                owner: definition.owner,
                mission: definition.mission,
                allowed_tools: definition.allowed_tools,
                model: definition.model,
                budgets: definition.budgets,
                level: definition.level,
                tool_rules: definition.tool_rules,
                triggers: definition.triggers,
                placement: definition.placement,
                groups: definition.groups,
                subscriptions: Vec::new(),
                status: BotStatus::Registered,
                created_at: now,
                updated_at: now,
            };
            s.bots.insert(id.clone(), bot.clone());
            s.bot_event("bot.registered", &id);
            Ok(bot)
        })
    }

    fn patch_bot(&self, id: BotId, patch: BotPatch) -> PortFuture<'_, Bot> {
        self.call(format!("patch_bot {id}"), move |s| {
            let now = s.clock();
            let bot = s
                .bots
                .get_mut(&id)
                .ok_or_else(|| PanelError::NotFound(format!("bot {id} not found")))?;
            if let Some(mission) = patch.mission {
                bot.mission = mission;
            }
            // The control plane names the levels when the dial turns.
            let mut payload = serde_json::Map::new();
            if let Some(level) = patch.level
                && level != bot.level
            {
                payload.insert("previousLevel".into(), bot.level.name().into());
                payload.insert("level".into(), level.name().into());
                bot.level = level;
            }
            if let Some(tools) = patch.allowed_tools {
                bot.allowed_tools = tools;
            }
            if let Some(placement) = patch.placement {
                bot.placement = placement;
            }
            if let Some(groups) = patch.groups {
                bot.groups = groups;
            }
            let subscribed = patch.subscriptions.is_some();
            if let Some(subscriptions) = patch.subscriptions {
                bot.subscriptions = subscriptions;
            }
            bot.updated_at = now;
            let bot = bot.clone();
            s.bot_event_with("bot.updated", &id, (!payload.is_empty()).then_some(payload));
            if subscribed {
                let list = serde_json::to_value(&bot.subscriptions).unwrap();
                let changed = serde_json::json!({"subscriptions": list, "by": "operator"});
                s.bot_event_with("bot.subscription.changed", &id, changed.as_object().cloned());
            }
            Ok(bot)
        })
    }

    fn enable_bot(&self, id: BotId) -> PortFuture<'_, Bot> {
        self.call(format!("enable_bot {id}"), move |s| s.switch(&id, BotStatus::Enabled))
    }

    fn pause_bot(&self, id: BotId) -> PortFuture<'_, Bot> {
        self.call(format!("pause_bot {id}"), move |s| s.switch(&id, BotStatus::Paused))
    }

    fn bot_events(&self, id: BotId, limit: u32) -> PortFuture<'_, Vec<Event>> {
        self.call(format!("bot_events {id}"), move |s| {
            s.bot(&id)?;
            let events: Vec<Event> = s.events.iter().filter(|e| e.bot_id == id.as_str()).cloned().collect();
            let skip = events.len().saturating_sub(limit as usize);
            Ok(events.into_iter().skip(skip).collect())
        })
    }

    fn group_events(&self, name: String, after: Option<String>, _wait_seconds: u32) -> PortFuture<'_, Vec<Event>> {
        self.call(format!("group_events {name}"), move |s| {
            let members: Vec<String> = s
                .bots
                .values()
                .filter(|b| b.groups.contains(&name))
                .map(|b| b.id.to_string())
                .collect();
            if members.is_empty() {
                return Err(PanelError::NotFound(format!("group {name} not found")));
            }
            let events: Vec<Event> = s
                .events
                .iter()
                .filter(|e| members.contains(&e.bot_id) || e.group_name == name)
                .cloned()
                .collect();
            let start = after
                .and_then(|cursor| events.iter().position(|e| e.event_id == cursor))
                .map(|i| i + 1)
                .unwrap_or(0);
            Ok(events.into_iter().skip(start).collect())
        })
    }

    fn group_topics(&self, name: String) -> PortFuture<'_, Vec<Topic>> {
        self.call(format!("group_topics {name}"), move |s| {
            if s.older {
                return Err(PanelError::NotFound("404 page not found".into()));
            }
            let members: Vec<Bot> = s.bots.values().filter(|b| b.groups.contains(&name)).cloned().collect();
            if members.is_empty() {
                return Err(PanelError::NotFound(format!("group {name} not found")));
            }
            let mut topics: BTreeMap<String, Topic> = BTreeMap::new();
            let blank = |name: &str| Topic {
                name: name.to_string(),
                publishers: Vec::new(),
                declared_publishers: Vec::new(),
                listeners: Vec::new(),
                last_at: None,
                count: 0,
                nobody_listens: false,
            };
            for event in &s.events {
                let ours = members.iter().any(|m| m.id.as_str() == event.bot_id) || event.group_name == name;
                if !ours || !matches!(event.event_type.as_str(), "bot.event.emitted" | "group.event.published") {
                    continue;
                }
                let Some(topic) = event.payload_text("name") else {
                    continue;
                };
                let from = event.payload_text("from").unwrap_or_default();
                let entry = topics.entry(topic.clone()).or_insert_with(|| blank(&topic));
                if !from.is_empty() && !entry.publishers.contains(&from) {
                    entry.publishers.push(from);
                }
                entry.count += 1;
                entry.last_at = Some(event.created_at);
            }
            for member in &members {
                for sub in &member.subscriptions {
                    topics.entry(sub.topic.clone()).or_insert_with(|| blank(&sub.topic));
                }
            }
            for topic in topics.values_mut() {
                for member in &members {
                    for sub in &member.subscriptions {
                        let fits =
                            sub.topic == topic.name || (topic.count > 0 && pattern_fits(&sub.topic, &topic.name));
                        if fits {
                            topic.listeners.push(TopicListener {
                                bot_id: member.id.to_string(),
                                topic: sub.topic.clone(),
                                mode: sub.mode().name().to_string(),
                                status: sub.status.clone(),
                            });
                        }
                    }
                }
                let kind = topic.name.split('.').next().unwrap_or_default();
                topic.nobody_listens = topic.count > 0
                    && !topic.listeners.iter().any(|l| l.status != "pending")
                    && kind != "stop"
                    && kind != "danger";
            }
            Ok(topics.into_values().collect())
        })
    }

    fn publish_event(&self, team: String, publication: Publication) -> PortFuture<'_, Published> {
        self.call(format!("publish_event {team} {}", publication.name), move |s| {
            if s.older {
                return Err(PanelError::NotFound("404 page not found".into()));
            }
            let kind = publication.name.split('.').next().unwrap_or_default();
            if kind == "error" || kind == "done" {
                return Err(PanelError::Invalid(format!(
                    "name: not_allowed: {kind}.* is the control plane's own"
                )));
            }
            let members: Vec<String> = s
                .bots
                .values()
                .filter(|b| b.groups.contains(&team))
                .map(|b| b.id.to_string())
                .collect();
            if members.is_empty() {
                return Err(PanelError::NotFound(format!("group {team} not found")));
            }
            let id = s.next("evt");
            let now = s.clock();
            let deliveries: Vec<serde_json::Value> = members
                .iter()
                .filter(|m| publication.to.is_empty() || publication.to.contains(m))
                .map(|m| serde_json::json!({"botId": m, "mode": "task", "created": true}))
                .collect();
            let payload = serde_json::json!({
                "name": publication.name, "message": publication.message, "from": "operator", "by": "operator",
                "to": publication.to, "group": team, "deliveries": deliveries, "eventId": id,
            });
            s.events.push(Event {
                event_id: id.clone(),
                event_type: "group.event.published".into(),
                tenant_id: "default".into(),
                bot_id: String::new(),
                node_id: String::new(),
                group_name: team.clone(),
                task_id: String::new(),
                execution_id: String::new(),
                incident_id: String::new(),
                correlation_id: String::new(),
                created_at: now,
                payload: payload.as_object().cloned(),
            });
            Ok(Published {
                event_id: id,
                name: publication.name,
                deliveries,
                skipped: Vec::new(),
                created: true,
            })
        })
    }

    fn list_tasks(&self, filter: TaskFilter) -> PortFuture<'_, Vec<Task>> {
        self.call("list_tasks".into(), move |s| {
            if let Some(bot) = &filter.bot {
                s.bot(bot)?;
            }
            // Newest first; of two made in the same instant, the later one.
            let mut tasks: Vec<(usize, Task)> = s
                .tasks
                .iter()
                .enumerate()
                .filter(|(_, t)| filter.bot.as_ref().is_none_or(|b| &t.bot_id == b))
                .filter(|(_, t)| filter.status.is_none_or(|st| t.status == st))
                .filter(|(_, t)| filter.since.is_none_or(|since| t.created_at >= since))
                .map(|(i, t)| (i, t.clone()))
                .collect();
            tasks.sort_by_key(|(i, t)| std::cmp::Reverse((t.created_at, *i)));
            let mut tasks: Vec<Task> = tasks.into_iter().map(|(_, t)| t).collect();
            tasks.truncate(filter.limit as usize);
            Ok(tasks)
        })
    }

    fn get_task(&self, id: TaskId) -> PortFuture<'_, Task> {
        self.call(format!("get_task {id}"), move |s| s.task_mut(&id).map(|t| t.clone()))
    }

    fn submit_task(&self, submission: TaskSubmission) -> PortFuture<'_, SubmittedTask> {
        self.call(format!("submit_task {}", submission.bot_id), move |s| {
            let bot = s.bot(&submission.bot_id)?;
            if let Some(task) = s
                .tasks
                .iter()
                .find(|t| t.bot_id == submission.bot_id && t.idempotency_key == submission.key.as_str())
            {
                return Ok(SubmittedTask {
                    created: false,
                    task: task.clone(),
                });
            }
            if !bot.status.accepts_tasks() {
                return Err(PanelError::State(format!("bot {} is {}", bot.id, bot.status.name())));
            }
            let now = s.clock();
            let id = s.add_task(
                submission.bot_id.as_str(),
                submission.objective.as_str(),
                TaskStatus::Pending,
                now,
            );
            let task = s.task_mut(&id)?;
            task.idempotency_key = submission.key.as_str().to_string();
            if submission.urgent {
                task.priority = "urgent".into();
            }
            Ok(SubmittedTask {
                created: true,
                task: task.clone(),
            })
        })
    }

    fn stop_bot(&self, id: BotId) -> PortFuture<'_, StoppedBotAnswer> {
        self.call(format!("stop_bot {id}"), move |s| {
            if s.older {
                return Err(PanelError::NotFound("404 page not found".into()));
            }
            let mut bot = s
                .bots
                .get(&id)
                .cloned()
                .ok_or_else(|| PanelError::NotFound(format!("bot {id} not found")))?;
            if bot.status.can_pause() {
                bot = s.switch(&id, BotStatus::Paused)?;
            }
            let now = s.clock();
            let mut cancelled = Vec::new();
            let open: Vec<TaskId> = s
                .tasks
                .iter()
                .filter(|t| t.bot_id == id && t.status.can_cancel())
                .map(|t| t.id.clone())
                .collect();
            for task_id in open {
                let task = s.task_mut(&task_id)?;
                task.status = TaskStatus::Cancelled;
                task.updated_at = now;
                s.event("bot.task.cancelled", id.as_str(), &task_id, now, serde_json::json!({}));
                cancelled.push(task_id.to_string());
            }
            Ok(StoppedBotAnswer {
                bot,
                cancelled,
                not_cancelled: Vec::new(),
            })
        })
    }

    fn send_task_message(&self, id: TaskId, text: String) -> PortFuture<'_, SentTaskMessage> {
        self.call(format!("send_task_message {id}"), move |s| {
            if s.older {
                return Err(PanelError::NotFound("404 page not found".into()));
            }
            let now = s.clock();
            let task = s.task_mut(&id)?.clone();
            if task.status != TaskStatus::Running {
                return Err(PanelError::State(format!("task {id} is {}", task.status.name())));
            }
            s.event(
                "bot.task.message",
                task.bot_id.as_str(),
                &id,
                now,
                serde_json::json!({ "text": text }),
            );
            Ok(SentTaskMessage {
                task_id: id,
                execution_id: "exec-run".into(),
                created_at: now,
            })
        })
    }

    fn cancel_task(&self, id: TaskId) -> PortFuture<'_, CancelledTask> {
        self.call(format!("cancel_task {id}"), move |s| {
            let now = s.clock();
            let task = s.task_mut(&id)?;
            match task.status {
                TaskStatus::Cancelled => Ok(CancelledTask {
                    task: task.clone(),
                    execution: None,
                    changed: false,
                }),
                TaskStatus::Completed | TaskStatus::Failed | TaskStatus::Expired => {
                    Err(PanelError::State(format!("task {id} is {}", task.status.name())))
                }
                _ => {
                    task.status = TaskStatus::Cancelled;
                    task.updated_at = now;
                    let task = task.clone();
                    let bot = task.bot_id.to_string();
                    s.event("bot.task.cancelled", &bot, &id, now, serde_json::json!({}));
                    Ok(CancelledTask {
                        task,
                        execution: None,
                        changed: true,
                    })
                }
            }
        })
    }

    fn decide_task(&self, id: TaskId, decision: Decision) -> PortFuture<'_, DecidedTask> {
        self.call(format!("decide_task {id} {}", decision.name()), move |s| {
            let now = s.clock();
            let task = s.task_mut(&id)?;
            if task.status != TaskStatus::WaitingApproval {
                return Err(PanelError::State(format!("task {id} is {}", task.status.name())));
            }
            task.status = TaskStatus::Running;
            let bot = task.bot_id.to_string();
            s.event(
                "bot.task.decided",
                &bot,
                &id,
                now,
                serde_json::json!({"decision": decision.name()}),
            );
            let exec = s.add_execution(&id, ExecutionOutcome::Running, Some("gx10-ff95"), "", None, now);
            let execution = s.executions.iter_mut().find(|e| e.id == exec).unwrap();
            execution.kind = ExecutionKind::Decide;
            execution.decision = decision.name().into();
            let execution = execution.clone();
            let task = s.task_mut(&id)?.clone();
            Ok(DecidedTask { task, execution })
        })
    }

    fn task_executions(&self, id: TaskId) -> PortFuture<'_, Vec<Execution>> {
        self.call(format!("task_executions {id}"), move |s| {
            s.task_mut(&id)?;
            Ok(s.executions.iter().filter(|e| e.task_id == id).cloned().collect())
        })
    }

    fn task_events(&self, id: TaskId, limit: u32) -> PortFuture<'_, Vec<Event>> {
        self.call(format!("task_events {id}"), move |s| {
            s.task_mut(&id)?;
            let events: Vec<Event> = s.events.iter().filter(|e| e.task_id == id.as_str()).cloned().collect();
            let skip = events.len().saturating_sub(limit as usize);
            Ok(events.into_iter().skip(skip).collect())
        })
    }
}

/// Whether a pattern (`*` one segment, `**` any) fits a name.
fn pattern_fits(pattern: &str, name: &str) -> bool {
    fn fits(p: &[&str], n: &[&str]) -> bool {
        match p.first() {
            None => n.is_empty(),
            Some(&"**") => (0..=n.len()).any(|i| fits(&p[1..], &n[i..])),
            Some(&"*") => !n.is_empty() && fits(&p[1..], &n[1..]),
            Some(segment) => n.first() == Some(segment) && fits(&p[1..], &n[1..]),
        }
    }
    let p: Vec<&str> = pattern.split('.').collect();
    let n: Vec<&str> = name.split('.').collect();
    fits(&p, &n)
}
