# Slack App Setup Guide

This guide walks you through creating and configuring the Slack app for Kagent Slackbot.

## Step 1: Create the Slack App

### Option A: Use the App Manifest (Recommended)

1. Go to https://api.slack.com/apps
2. Click **"Create New App"**
3. Select **"From an app manifest"**
4. Choose your workspace
5. Select **YAML** tab
6. Copy and paste the contents of `slack-app-manifest.yaml`
7. Click **"Next"** → Review → **"Create"**

The manifest will create an app named **"kagent-bot"** with all necessary scopes and settings.

### Option B: Manual Configuration

1. Go to https://api.slack.com/apps
2. Click **"Create New App"** → **"From scratch"**
3. Name: `kagent-bot`
4. Choose your workspace
5. Continue with manual configuration below

## Step 2: Configure OAuth & Permissions

Go to **OAuth & Permissions** in the sidebar:

### Bot Token Scopes

Add these scopes:
- `app_mentions:read` - Listen for @mentions
- `chat:write` - Send messages
- `commands` - Handle slash commands
- `reactions:write` - Add emoji reactions
- `im:history` - Receive direct messages
- `users:read` - View user profiles (for RBAC)
- `users:read.email` - View user emails (for RBAC)

Click **"Save Changes"**

## Step 3: Enable Socket Mode

Go to **Socket Mode** in the sidebar:

1. Toggle **"Enable Socket Mode"** to **ON**
2. Give your token a name: `kagent-socket-token`
3. Click **"Generate"**
4. **SAVE THIS TOKEN** - you'll need it for `SLACK_APP_TOKEN`
   - Format: `xapp-1-...`
   - This is your **App-Level Token**

## Step 4: Configure Event Subscriptions

Go to **Event Subscriptions** in the sidebar:

1. Toggle **"Enable Events"** to **ON**
2. Under **"Subscribe to bot events"**, add:
   - `app_mention` - For @mentions in channels
   - `message.im` - For direct messages

Note: With Socket Mode, you don't need a Request URL!

## Step 5: Create Slash Commands

Go to **Slash Commands** in the sidebar:

### Command 1: /agents
- **Command**: `/agents`
- **Short Description**: `List available Kagent agents`
- **Usage Hint**: `[no parameters]`
- Click **"Save"**

### Command 2: /agent-switch
- **Command**: `/agent-switch`
- **Short Description**: `Switch to a specific agent or reset to auto-routing`
- **Usage Hint**: `<namespace>/<name> or reset`
- Click **"Save"**

## Step 6: Install App to Workspace

1. Go to **Install App** in the sidebar
2. Click **"Install to Workspace"**
3. Review permissions
4. Click **"Allow"**

## Step 7: Collect Your Tokens

You'll need **3 tokens** for the bot:

### 1. Bot User OAuth Token
- Go to **OAuth & Permissions**
- Copy **"Bot User OAuth Token"**
- Format: `xoxb-...`
- Save as: `SLACK_BOT_TOKEN`

### 2. App-Level Token
- Go to **Basic Information** → **App-Level Tokens**
- Copy the token you created in Step 3
- Format: `xapp-1-...`
- Save as: `SLACK_APP_TOKEN`

### 3. Signing Secret
- Go to **Basic Information** → **App Credentials**
- Copy **"Signing Secret"**
- Format: alphanumeric string
- Save as: `SLACK_SIGNING_SECRET`

## Step 8: Configure the Bot

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

## Step 9: Invite Bot to Channels

In Slack:

1. Go to any channel where you want to use the bot
2. Type: `/invite @kagent-bot`
3. Or mention it: `@kagent-bot` (it will prompt you to invite it)

**Or** just DM the bot directly (no invite needed for DMs)

## Step 10: Test the Bot

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
- [ ] Subscribed to `app_mention` event
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
