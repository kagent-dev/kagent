# Slack App Setup Guide

This guide walks you through creating and configuring the Slack app for Kagent Slackbot.

## Option A: Quick Setup with App Manifest (Recommended)

Using the app manifest automatically configures everything for you.

### Steps:

1. Go to https://api.slack.com/apps
2. Click **"Create New App"** → **"From an app manifest"**
3. Choose your workspace
4. Select **YAML** tab
5. Copy and paste the contents of `slack-app-manifest.yaml`
6. Click **"Next"** → Review → **"Create"**

The manifest creates an app named **"kagent-bot"** with all scopes, Socket Mode, event subscriptions, and slash commands pre-configured.

### Generate Socket Mode Token:

Socket Mode is already enabled by the manifest. You just need to generate a token:

1. Go to **Socket Mode** in the sidebar
2. Verify the toggle is **ON** (it should already be enabled)
3. Under **App-Level Tokens**, click **"Generate"** to create a new token
4. Name it: `kagent-socket-token`
5. **SAVE THIS TOKEN** - you'll need it for `SLACK_APP_TOKEN` (format: `xapp-1-...`)

### Install the App:

1. Go to **Install App** in the sidebar
2. Click **"Install to Workspace"**
3. Review permissions and click **"Allow"**

### Skip to Step 3 below to collect your tokens.

---

## Option B: Manual Configuration

If you prefer to configure everything manually instead of using the manifest.

### Step 1: Create the App

1. Go to https://api.slack.com/apps
2. Click **"Create New App"** → **"From scratch"**
3. Name: `kagent-bot`
4. Choose your workspace
5. Click **"Create App"**

### Step 2: Configure OAuth & Permissions (Manual Setup Only)

Go to **OAuth & Permissions** in the sidebar and add these bot token scopes:

- `app_mentions:read` - Listen for @mentions
- `chat:write` - Send messages
- `commands` - Handle slash commands
- `reactions:write` - Add emoji reactions
- `im:history` - Receive direct messages
- `users:read` - View user profiles (for RBAC)
- `users:read.email` - View user emails (for RBAC)
- `usergroups:read` - Read user groups (for RBAC)

Click **"Save Changes"**

### Step 3: Enable Socket Mode (Manual Setup Only)

Go to **Socket Mode** in the sidebar:

1. Toggle **"Enable Socket Mode"** to **ON**
2. Under **App-Level Tokens**, give your token a name: `kagent-socket-token`
3. Click **"Generate"**
4. **SAVE THIS TOKEN** - you'll need it for `SLACK_APP_TOKEN` (format: `xapp-1-...`)

### Step 4: Configure Event Subscriptions (Manual Setup Only)

Go to **Event Subscriptions** in the sidebar:

1. Toggle **"Enable Events"** to **ON**
2. Under **"Subscribe to bot events"**, add:
   - `app_mention` - For @mentions in channels
   - `message.im` - For direct messages

Note: With Socket Mode, you don't need a Request URL!

### Step 5: Enable Interactivity (Manual Setup Only)

Go to **Interactivity & Shortcuts** in the sidebar:

1. Toggle **"Interactivity"** to **ON**
2. **No Request URL needed** (Socket Mode handles this automatically)
3. Click **"Save Changes"**

This enables button actions for HITL approval workflows.

### Step 6: Create Slash Commands (Manual Setup Only)

Go to **Slash Commands** in the sidebar and create two commands:

**Command 1: /agents**
- **Command**: `/agents`
- **Short Description**: `List available Kagent agents`
- **Usage Hint**: `[no parameters]`
- Click **"Save"**

**Command 2: /agent-switch**
- **Command**: `/agent-switch`
- **Short Description**: `Switch to a specific agent or reset to auto-routing`
- **Usage Hint**: `<namespace>/<name> or reset`
- Click **"Save"**

### Step 7: Install App to Workspace (Manual Setup Only)

1. Go to **Install App** in the sidebar
2. Click **"Install to Workspace"**
3. Review permissions and click **"Allow"**

---

## Collect Your Tokens (Required for Both Options)

After creating and installing the app (via either option above), you'll need **3 tokens**:

### 1. Bot User OAuth Token
- Go to **OAuth & Permissions** in the sidebar
- Copy **"Bot User OAuth Token"**
- Format: `xoxb-...`
- Save as: `SLACK_BOT_TOKEN`

### 2. App-Level Token (Socket Mode)
- Go to **Basic Information** → **App-Level Tokens**
- Copy the token you generated
- Format: `xapp-1-...`
- Save as: `SLACK_APP_TOKEN`

### 3. Signing Secret
- Go to **Basic Information** → **App Credentials**
- Copy **"Signing Secret"**
- Format: alphanumeric string
- Save as: `SLACK_SIGNING_SECRET`

## Configure the Bot (Required for Both Options)

Create a `.env` file in the slackbot directory:

```bash
cd /path/to/kagent/slackbot
cp .env.example .env
```

Edit `.env` with your tokens:

```bash
# Slack Configuration
SLACK_BOT_TOKEN=xoxb-your-bot-token-here
SLACK_APP_TOKEN=xapp-1-your-app-token-here
SLACK_SIGNING_SECRET=your-signing-secret-here

# Kagent Configuration
KAGENT_BASE_URL=http://localhost:8083
KAGENT_TIMEOUT=30

# Server Configuration
SERVER_HOST=0.0.0.0
SERVER_PORT=8080

# Logging
LOG_LEVEL=INFO
```

## Invite Bot to Channels (Required for Both Options)

In Slack:

1. Go to any channel where you want to use the bot
2. Type: `/invite @kagent-bot`
3. Or mention it: `@kagent-bot` (it will prompt you to invite it)

**Or** just DM the bot directly (no invite needed for DMs)

## Test the Bot (Required for Both Options)

### Start the bot locally:

```bash
cd /path/to/kagent/slackbot
source .venv/bin/activate
python -m kagent_slackbot.main
```

You should see:
```json
{"event": "Starting Kagent Slackbot", "level": "info", ...}
{"event": "Health server started", "host": "0.0.0.0", "port": 8080, ...}
{"event": "Connecting to Slack via Socket Mode", ...}
```

### Test in Slack:

1. **Test agent list**:
   ```
   /agents
   ```
   Should show available agents from kagent

2. **Test @mention**:
   ```
   @kagent-bot hello
   ```
   Should get a formatted response from an agent

3. **Test DM**:
   - DM the bot directly: "show me kubernetes pods"
   - Should respond without needing @mention

4. **Test agent switching**:
   ```
   /agent-switch kagent/k8s-agent
   ```
   Should confirm the switch

## Troubleshooting

### "Invalid token" error

- Double-check you copied the tokens correctly
- Ensure no extra spaces or newlines
- Bot token should start with `xoxb-`
- App token should start with `xapp-`

### Bot doesn't respond to @mentions

- Check bot is invited to the channel (`/invite @kagent-bot`)
- Verify Socket Mode is enabled
- Check bot logs for connection errors
- Ensure `app_mention` and `message.im` event subscriptions are configured

### "Missing required scopes" error

- Reinstall the app to workspace
- Go to OAuth & Permissions → Reinstall App

### Commands not showing up

- Slash commands can take a few minutes to propagate
- Try logging out and back into Slack
- Check if commands show up in the `/` menu

## Quick Checklist

- [ ] Created Slack app
- [ ] Enabled Socket Mode
- [ ] Added bot scopes: `app_mentions:read`, `chat:write`, `commands`, `reactions:write`
- [ ] Subscribed to `app_mention` and `message.im` events
- [ ] Enabled Interactivity (for HITL approval buttons)
- [ ] Created `/agents` slash command
- [ ] Created `/agent-switch` slash command
- [ ] Installed app to workspace
- [ ] Copied all 3 tokens
- [ ] Created `.env` file with tokens
- [ ] Invited bot to test channel
- [ ] Bot started successfully
- [ ] Bot responds to @mentions

## Next Steps

Once the bot is working:
- Set up Kubernetes deployment (see README.md)
- Configure RBAC with Slack user groups (see README.md for permissions configuration)
