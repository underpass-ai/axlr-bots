// Package logline turns the control plane's log lines into structured
// records for Loki: with --log-format json each line is one JSON object
// with its time, level, message and the fields read from it (the
// component before the first colon, the task, execution and incident
// ids it names). The text format stays the default and is unchanged.
// It is the standard library's log/slog; no dependency.
package logline
