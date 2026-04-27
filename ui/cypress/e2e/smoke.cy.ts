describe('Onboarding Wizard', () => {
  it('successfully loads the first page of the onboarding wizard', () => {
    cy.window().then((win) => {
        win.localStorage.setItem('kagent-onboarding', 'false');
      })

    cy.visit('/')

    cy.contains('p', "Let's get you started by creating your first agent")
    cy.contains('button', "Let's Get Started").click();


    cy.contains('div', "Step 1: Configure AI Model").should('be.visible');
    cy.get('button[role="combobox"]').should('be.visible');
    cy.contains('button', 'Next: Agent Setup').should('be.visible');


    cy.contains('label', 'Create New').should('be.visible').click();
    cy.contains('label', "Provider & Model").should('be.visible');
    cy.contains('label', "Configuration Name").should('be.visible');
  })
})

describe('Main page', () => {
  it('successfully loads the main page', () => {
    cy.window().then((win) => {
      win.localStorage.setItem('kagent-onboarding', 'true');
    })

    cy.visit('/')
    cy.contains('h1', 'Agents').should('be.visible');

    cy.wait(1000)
    cy.visit('/agents')
    cy.contains('h1', 'Agents').should('be.visible');

    cy.visit('/agents/new')
    cy.contains('h1', 'New Agent').should('be.visible');

    cy.wait(1000)
    cy.visit('/models')
    cy.contains('h1', 'Models').should('be.visible');

    cy.visit('/models/new')
    cy.contains('h1', 'New Model').should('be.visible');

    cy.wait(1000)
    cy.visit('/mcp')
    cy.contains('h1', 'MCP & tools').should('be.visible');
    cy.get('#mcp-search').should('be.visible');
  })
})


describe('Regressions', () => {
  it('model edit page should load correctly', () => {
    cy.window().then((win) => {
      win.localStorage.setItem('kagent-onboarding', 'true');
    })

    cy.visit('/models')
    cy.contains('h1', 'Models').should('be.visible');

    // `model.ref` (e.g. default/default-model-config) is embedded in data-test; use prefix to avoid exact-ref coupling
    cy.get('[data-test^="edit-model-"]')
      .first()
      .should('be.visible')
      .click();

    cy.contains('h1', 'Edit Model').should('be.visible');
    cy.get('[data-test="edit-model-name-button"]').should('be.visible').click();
  })
})