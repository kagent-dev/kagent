import { NextResponse } from 'next/server';

// In-memory store for demo purposes only. Replace with real DB in production.
let users = [
  { id: '1', email: 'admin@example.com', name: 'Admin User', role: 'admin', organizationId: '1', createdAt: new Date().toISOString() },
  { id: '2', email: 'user@example.com', name: 'Regular User', role: 'user', organizationId: '2', createdAt: new Date().toISOString() },
];

export async function GET() {
  return NextResponse.json(users);
}

export async function POST(request: Request) {
  try {
    const body = await request.json();
    const { email, name, password, role, organizationId } = body || {};

    if (!email || !name || !password || !role) {
      return NextResponse.json({ message: 'Missing required fields' }, { status: 400 });
    }

    if (users.some(u => u.email.toLowerCase() === email.toLowerCase())) {
      return NextResponse.json({ message: 'Email already exists' }, { status: 409 });
    }

    const id = (Math.max(...users.map(u => Number(u.id))) + 1 || 1).toString();
    const newUser = { id, email, name, role, organizationId, createdAt: new Date().toISOString() };
    users.push(newUser);

    // Never return password
    return NextResponse.json(newUser, { status: 201 });
  } catch (e) {
    return NextResponse.json({ message: 'Invalid request' }, { status: 400 });
  }
}

// Export the store for other route handlers (edit/delete)
export const __usersStore = {
  get: () => users,
  set: (next: typeof users) => { users = next; },
};
