import { NextResponse } from 'next/server';

// In-memory store for demo purposes only. Replace with real DB in production.
let organizations = [
  {
    id: '1',
    name: 'Acme Corporation',
    domain: 'acme.com',
    description: 'Leading technology solutions provider',
    adminCount: 2,
    userCount: 15,
    createdAt: new Date().toISOString(),
    status: 'active'
  },
  {
    id: '2',
    name: 'TechStart Inc',
    domain: 'techstart.io',
    description: 'Innovative startup in AI space',
    adminCount: 1,
    userCount: 8,
    createdAt: new Date(Date.now() - 7 * 24 * 60 * 60 * 1000).toISOString(), // 7 days ago
    status: 'active'
  }
];

export async function GET() {
  return NextResponse.json({ organizations });
}

export async function POST(request: Request) {
  try {
    const body = await request.json();
    const { name, domain, description, admin } = body || {};

    if (!name || !domain || !admin) {
      return NextResponse.json({ error: 'Missing required fields: name, domain, admin' }, { status: 400 });
    }

    if (!admin.name || !admin.email || !admin.password) {
      return NextResponse.json({ error: 'Admin information incomplete' }, { status: 400 });
    }

    if (organizations.some(org => org.domain.toLowerCase() === domain.toLowerCase())) {
      return NextResponse.json({ error: 'Domain already exists' }, { status: 409 });
    }

    const id = (Math.max(...organizations.map(org => Number(org.id))) + 1 || 1).toString();
    const newOrganization = {
      id,
      name,
      domain,
      description,
      adminCount: 1,
      userCount: 1, // Admin user
      createdAt: new Date().toISOString(),
      status: 'active'
    };

    organizations.push(newOrganization);

    return NextResponse.json(newOrganization, { status: 201 });
  } catch (e) {
    return NextResponse.json({ error: 'Invalid request' }, { status: 400 });
  }
}

// Export the store for other route handlers
export const __organizationsStore = {
  get: () => organizations,
  set: (next: typeof organizations) => { organizations = next; },
};
