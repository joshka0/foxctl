# foxctl MCP Daemon - launchd Configuration

Auto-start the MCP SSE daemon on macOS using launchd.

## Installation

1. **Copy the plist to LaunchAgents:**
   ```bash
   cp configs/launchd/com.foxctl.mcp-daemon.plist ~/Library/LaunchAgents/
   ```

2. **Edit the plist to set your foxctl path:**
   ```bash
   # If using homebrew or custom install location:
   sed -i '' "s|/usr/local/bin/foxctl|$(which foxctl)|" \
       ~/Library/LaunchAgents/com.foxctl.mcp-daemon.plist
   ```

3. **Load the service:**
   ```bash
   launchctl load ~/Library/LaunchAgents/com.foxctl.mcp-daemon.plist
   ```

4. **Verify it's running:**
   ```bash
   curl -s http://localhost:8092/health | jq .
   ```

## Management Commands

```bash
# Check status
launchctl list | grep foxctl

# Stop the daemon
launchctl stop com.foxctl.mcp-daemon

# Start the daemon
launchctl start com.foxctl.mcp-daemon

# Unload (disable auto-start)
launchctl unload ~/Library/LaunchAgents/com.foxctl.mcp-daemon.plist

# Reload after editing plist
launchctl unload ~/Library/LaunchAgents/com.foxctl.mcp-daemon.plist
launchctl load ~/Library/LaunchAgents/com.foxctl.mcp-daemon.plist

# View logs
tail -f /tmp/foxctl-mcp-daemon.stderr.log
```

## Customization

### Change the port
Edit the plist and change the `--http` argument:
```xml
<string>--http</string>
<string>:9000</string>
```

### Add environment variables
Add keys to the `EnvironmentVariables` dict:
```xml
<key>EnvironmentVariables</key>
<dict>
    <key>FOXCTL_EMBEDDING_BASE_URL</key>
    <string>http://127.0.0.1:1234/v1</string>
</dict>
```

### Move logs to a better location
```xml
<key>StandardOutPath</key>
<string>/Users/YOUR_USER/.foxctl/logs/mcp-daemon.stdout.log</string>
<key>StandardErrorPath</key>
<string>/Users/YOUR_USER/.foxctl/logs/mcp-daemon.stderr.log</string>
```

## Troubleshooting

### Daemon won't start
1. Check the error log: `cat /tmp/foxctl-mcp-daemon.stderr.log`
2. Verify binary path: `which foxctl`
3. Test manually: `foxctl mcp serve --http :8092 --skills`

### Port already in use
Check what's using port 8092:
```bash
lsof -i :8092
```

Kill existing daemon:
```bash
# Via PID file
kill $(cat ~/.foxctl/mcp-daemon.pid)

# Or via lsof
kill $(lsof -t -i :8092)
```

### Daemon keeps restarting
The `KeepAlive` setting restarts on crash. If it's crash-looping:
1. Check logs for the error
2. Temporarily disable with `launchctl unload`
3. Fix the issue and reload
