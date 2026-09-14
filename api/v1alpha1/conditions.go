package v1alpha1

// Every kind reports its state through the same two conditions, with the
// meaning CONCEPT.md gives them:
//
//	Valid   the declared configuration holds together: what it names exists and
//	        does not contradict itself
//	Ready   what it names is currently available
//
// The split is what lets an alert tell a typo from an outage. A tool set naming
// a server that does not exist is Valid=False and stays that way until someone
// edits a manifest; one whose server is down is Valid=True, Ready=False and
// recovers on its own.
//
// MCPServer carries Ready only. Its spec is an endpoint and how to authenticate
// to it, which the schema already checks; whether the Secrets it names exist and
// the credentials in them are accepted is only found out by using them, and that
// is what Ready reports.
const (
	ConditionValid = "Valid"
	ConditionReady = "Ready"
)

// Reasons. They are part of the API: an alert rule or a kubectl plugin matches
// on them, so a reason once published is not renamed.
const (
	// ReasonDiscovered: the server answered tools/list.
	ReasonDiscovered = "Discovered"
	// ReasonDiscoveryFailed: the server could not be reached, or did not
	// answer as an MCP server.
	ReasonDiscoveryFailed = "DiscoveryFailed"
	// ReasonUnauthorized: the server refused the credentials spec.auth
	// sends, or requires credentials and spec.auth sends none.
	ReasonUnauthorized = "Unauthorized"
	// ReasonCredentialsUnavailable: the credentials spec.auth names could not
	// be obtained — a Secret or one of its keys is missing, or the token
	// endpoint refused the client.
	ReasonCredentialsUnavailable = "CredentialsUnavailable"

	// ReasonResolved: every reference names an existing resource.
	ReasonResolved = "Resolved"
	// ReasonServerNotFound: spec.serverRef names no MCPServer.
	ReasonServerNotFound = "ServerNotFound"
	// ReasonToolSetNotFound: spec.toolSets names an MCPToolSet that does not exist.
	ReasonToolSetNotFound = "ToolSetNotFound"
	// ReasonCapabilityNotFound: spec.capabilities names an MCPCapability that
	// does not exist.
	ReasonCapabilityNotFound = "CapabilityNotFound"
	// ReasonToolNameConflict: two tools from different servers would be
	// exposed under the same name. The gateway exposes neither.
	ReasonToolNameConflict = "ToolNameConflict"

	// ReasonAvailable: everything referenced is currently available.
	ReasonAvailable = "Available"
	// ReasonServerUnavailable: the referenced server is not Ready.
	ReasonServerUnavailable = "ServerUnavailable"
	// ReasonCatalogPending: the server is Ready, but this controller has not
	// read its catalog since it started.
	ReasonCatalogPending = "CatalogPending"
	// ReasonMissingTools: the server is up and does not expose some of the
	// declared tools.
	ReasonMissingTools = "MissingTools"
	// ReasonUnmatchedPatterns: the server is up and some of the declared
	// patterns select none of its tools.
	ReasonUnmatchedPatterns = "UnmatchedPatterns"
	// ReasonToolSetNotReady: a referenced tool set is not Ready.
	ReasonToolSetNotReady = "ToolSetNotReady"
	// ReasonCapabilityNotReady: a referenced capability is not Ready.
	ReasonCapabilityNotReady = "CapabilityNotReady"
	// ReasonInvalid: the resource is Valid=False, so it cannot be Ready either.
	ReasonInvalid = "Invalid"
)
