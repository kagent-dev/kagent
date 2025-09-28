# Quick Start Guide

## Installation

### Prerequisites
- Node.js 18 or later
- npm (comes with Node.js) or yarn
- Git

### Clone the Repository
```bash
git clone https://github.com/your-org/adolphe-ai.git
cd adolphe-ai
```

### Install Dependencies
```bash
npm install
# or
yarn install
```

## Configuration

1. Copy the example environment file:
   ```bash
   cp .env.example .env.local
   ```

2. Update the environment variables in `.env.local` with your configuration.

## Running the Application

### Development Mode
```bash
npm run dev
# or
yarn dev
```

Open [http://localhost:3000](http://localhost:3000) in your browser.

### Production Build
```bash
npm run build
npm start
# or
yarn build
yarn start
```

## Your First Agent

1. Navigate to the Agents section in the dashboard
2. Click "Create New Agent"
3. Fill in the agent details:
   - Name: My First Agent
   - Description: A simple demo agent
   - Model: gpt-4
   - System Prompt: "You are a helpful assistant."

4. Save the agent
5. Start chatting with your new agent!

## Next Steps

- Explore the [Agents documentation](./agents.md) for advanced configurations
- Check out the [API Reference](./api.md) for integration options
- Read the [Development Guide](./development.md) for contributing to the project

## Getting Help

- [Documentation](https://docs.adolphe.ai)
- [Community Forum](https://community.adolphe.ai)
- [Support](mailto:support@adolphe.ai)
