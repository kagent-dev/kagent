import { NextResponse } from 'next/server';
import { __usersStore } from '../route';

export async function GET(
  _request: Request,
  { params }: { params: { id: string } }
) {
  const users = __usersStore.get();
  const user = users.find(u => u.id === params.id);
  if (!user) return NextResponse.json({ message: 'Not found' }, { status: 404 });
  return NextResponse.json(user);
}

export async function PUT(
  request: Request,
  { params }: { params: { id: string } }
) {
  try {
    const payload = await request.json();
    const users = __usersStore.get();
    const idx = users.findIndex(u => u.id === params.id);
    if (idx === -1) return NextResponse.json({ message: 'Not found' }, { status: 404 });

    const { email, name, role, organizationId } = payload || {};
    if (!email || !name || !role) {
      return NextResponse.json({ message: 'Missing required fields' }, { status: 400 });
    }

    // ensure unique email for others
    if (users.some(u => u.email.toLowerCase() === email.toLowerCase() && u.id !== params.id)) {
      return NextResponse.json({ message: 'Email already exists' }, { status: 409 });
    }

    const updated = { ...users[idx], email, name, role, organizationId };
    const next = [...users];
    next[idx] = updated;
    __usersStore.set(next);

    return NextResponse.json(updated);
  } catch (e) {
    return NextResponse.json({ message: 'Invalid request' }, { status: 400 });
  }
}

export async function DELETE(
  _request: Request,
  { params }: { params: { id: string } }
) {
  const users = __usersStore.get();
  const idx = users.findIndex(u => u.id === params.id);
  if (idx === -1) return NextResponse.json({ message: 'Not found' }, { status: 404 });

  const next = users.filter(u => u.id !== params.id);
  __usersStore.set(next);
  return NextResponse.json({ ok: true });
}
