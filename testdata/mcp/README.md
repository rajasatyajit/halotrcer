# MCP client configuration for the smoke matrix

`smoke-mcp.json` is the configuration `halotrcer check --mcp-config` hands to
Sievant in the smoke matrix (PLAN.md M4). It follows the `mcpServers` format
Sievant documents for Claude Desktop and Cursor (sievant docs/gateway.md),
written for Halotrcer; it is not one of Sievant's own test files.

Every value is fake. The two `smoke-canary-…` strings are planted on purpose:
the smoke run fails if either appears in the combined report, which proves
Sievant's masking is preserved end to end. With the stand-in tools (PLAN.md
K1) Sievant does not read the file; with real releases it does.
