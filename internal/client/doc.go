// Package client is a BookStack REST API client.
//
// It must not depend on the CLI layer (cobra) nor write to stdout or stderr,
// so it can be reused by other front ends such as an MCP server.
package client
