# Development Guide

## Project Structure

```
/adolphe-ai
├── /ui                   # Frontend application
│   ├── /src
│   │   ├── /app          # Next.js app router
│   │   ├── /components   # Reusable UI components
│   │   └── /lib          # Shared utilities
│   └── package.json
│
├── /server              # Backend services
│   ├── /src
│   │   ├── /api         # API routes
│   │   ├── /agents      # Agent implementations
│   │   └── /models      # Data models
│   └── package.json
│
└── /docs               # Documentation
    ├── api.md
    ├── agents.md
    └── development.md
```

## Setup Instructions

### Prerequisites
- Node.js 18+
- npm or yarn
- PostgreSQL
- Redis (for caching)

### Environment Variables
Create a `.env` file in the root directory:
```env
# Database
DATABASE_URL=postgresql://user:password@localhost:5432/adolphe
REDIS_URL=redis://localhost:6379

# Authentication
NEXTAUTH_SECRET=your-secret-key
NEXTAUTH_URL=http://localhost:3000

# AI Providers
OPENAI_API_KEY=your-openai-key
ANTHROPIC_API_KEY=your-anthropic-key
```

## Development Workflow

### Starting the Development Server
```bash
# Install dependencies
npm install

# Start the development server
npm run dev

# Run tests
npm test

# Build for production
npm run build
```

### Code Style
We use ESLint and Prettier for code formatting:
```bash
# Check for linting errors
npm run lint

# Format code
npm run format
```

## Testing

### Unit Tests
```bash
# Run all tests
npm test

# Run tests in watch mode
npm test -- --watch

# Run specific test file
npm test -- src/utils/__tests__/format.test.ts
```

### Integration Tests
```bash
# Run API tests
npm run test:api

# Run end-to-end tests
npm run test:e2e
```

## Contributing

### Branch Naming
- `feature/`: New features
- `fix/`: Bug fixes
- `docs/`: Documentation changes
- `refactor/`: Code refactoring
- `chore/`: Maintenance tasks

### Pull Request Process
1. Fork the repository
2. Create your feature branch
3. Commit your changes
4. Push to the branch
5. Open a pull request

## Deployment

### Staging
```bash
# Deploy to staging
npm run deploy:staging
```

### Production
```bash
# Deploy to production
npm run deploy:production
```

## Troubleshooting

### Common Issues
1. **Database connection failed**
   - Verify PostgreSQL is running
   - Check connection string in `.env`

2. **Missing environment variables**
   - Ensure all required variables are set in `.env`
   - Restart the development server after changes

3. **Build errors**
   - Clear Next.js cache: `rm -rf .next`
   - Reinstall dependencies: `rm -rf node_modules && npm install`

## Support
For additional help, please contact the development team or open an issue on GitHub.
