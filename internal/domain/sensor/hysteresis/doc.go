// Package hysteresis is the pure physical state machine of a sensor (m12-plan §5): a confirmed state changes after
// N agreeing readings or after T since the first of them; silence makes it unknown. No store, no clock, no float:
// instants are passed in. The change instant is the first agreeing reading.
package hysteresis
