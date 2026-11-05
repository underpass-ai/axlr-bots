use crate::domain::BotId;

/// Which words of a detail page a reader holds.
#[derive(Debug, Clone, PartialEq, Eq, Hash)]
pub enum ReaderKey {
    BotMission,
    BotRunsOn,
    BotModel,
    BotBudgets,
    /// What a member of the team on screen is for.
    MemberPurpose(BotId),
    IncidentTitle,
    IncidentReason,
    IncidentDiagnosis,
    IncidentSource,
    IncidentGroup,
    /// One alert of the incident on screen, by its fingerprint.
    Fingerprint(String),
    MintedNode,
    MintedToken,
    /// The new task's key, which makes a second send the same task.
    SubmitKey,
}
