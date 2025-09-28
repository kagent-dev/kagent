import { NextResponse } from 'next/server';

// Mock user organizations data
// In a real app, this would be based on the authenticated user's organizations
const mockUserOrganizations = [
  {
    id: '1',
    name: 'Acme Corporation',
    domain: 'acme.com',
    description: 'Leading technology solutions provider',
    role: 'admin',
    status: 'active'
  },
  {
    id: '2',
    name: 'TechStart Inc',
    domain: 'techstart.io',
    description: 'Innovative startup in AI space',
    role: 'user',
    status: 'active'
  }
];

export async function GET() {
  // In a real app, this would check authentication and return user's organizations
  return NextResponse.json({
    organizations: mockUserOrganizations,
    currentOrganizationId: '1' // Default to first organization
  });
}
