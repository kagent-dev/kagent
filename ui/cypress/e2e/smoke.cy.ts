describe('Onboarding Wizard', () => {
  it('successfully loads the first page of the onboarding wizard', () => {
    cy.visit('/', {
      onBeforeLoad(win) {
        win.localStorage.setItem('kagent-onboarding', 'false');
      },
    })

    cy.contains('p', "Let's get you started by creating your first agent")
    cy.contains('button', "Let's Get Started").click();


    cy.contains('body', /Step 1: Configure AI Model|Failed to load configurations/i).should('be.visible');
  })
})

describe('Main page', () => {
  it('successfully loads the main page', () => {
    cy.visit('/', {
      onBeforeLoad(win) {
        win.localStorage.setItem('kagent-onboarding', 'true');
      },
    })
    cy.contains('body', /Agents|fetch failed/i).should('be.visible');

    cy.wait(1000)
    cy.visit('/agents')
    cy.contains('body', /Agents|fetch failed/i).should('be.visible');

    cy.visit('/agents/new')
    cy.contains('h1', 'Create New Agent').should('be.visible');

    cy.wait(1000)
    cy.visit('/models')
    cy.contains('h1', 'Models').should('be.visible');

    cy.visit('/models/new')
    cy.url().should('include', '/models/new');

    cy.wait(1000)
    cy.visit('/tools')
    cy.contains('h1', 'Tools Library').should('be.visible');

    cy.wait(1000)
    cy.visit('/servers')
    cy.contains('h1', 'MCP Servers').should('be.visible');
  })
})


describe('Plugins', () => {
  it('plugins/kanban page loads with plugin shell and iframe', () => {
    cy.visit('/plugins/kanban', {
      onBeforeLoad(win) {
        win.localStorage.setItem('kagent-onboarding', 'true');
      },
    });
    // Plugin page renders with iframe that loads plugin content via /_p/kanban/
    cy.get('iframe[title="Plugin: kanban"]', { timeout: 10000 }).should('exist');
    cy.get('iframe[title="Plugin: kanban"]').should('have.attr', 'src').and('include', '/_p/kanban');
  });
});

describe('Regressions', () => {
  it('model edit page should load correctly', () => {
    cy.visit('/models', {
      onBeforeLoad(win) {
        win.localStorage.setItem('kagent-onboarding', 'true');
      },
    })
    cy.contains('h1', 'Models').should('be.visible');
    cy.contains('button', 'New Model').should('be.visible');
  })
})