import { NextApiRequest, NextApiResponse } from 'next';
import jwt from 'jsonwebtoken';

// WARNING: In production, load this from a secure source!
const JWT_SECRET = process.env.A2A_JWT_SECRET;
const VALID_ALGORITHMS = ['HS256', 'HS384', 'HS512'];

// List of agents that have authentication disabled
const AUTH_DISABLED_AGENTS = process.env.AUTH_DISABLED_AGENTS?.split(',') || [];

export default async function handler(req: NextApiRequest, res: NextApiResponse) {
  console.log('Received request to /api/generate-a2a-token:', req.method, req.body);
  
  if (req.method !== 'POST') {
    console.warn('Method not allowed:', req.method);
    return res.status(405).json({ error: 'Method not allowed' });
  }

  const { audience, issuer, type, expiresIn, agentName } = req.body;
  
  // Check if this agent has authentication disabled
  if (agentName && AUTH_DISABLED_AGENTS.includes(agentName)) {
    console.log(`Authentication is disabled for agent: ${agentName}`);
    return res.status(200).json({ 
      authDisabled: true,
      message: `Authentication is disabled for agent: ${agentName}`
    });
  }

  if (!JWT_SECRET) {
    console.error('A2A_JWT_SECRET is not set on the server.');
    return res.status(500).json({ error: 'A2A_JWT_SECRET is not set on the server.' });
  }
  
  if (!audience || !issuer) {
    console.warn('Missing audience or issuer:', req.body);
    return res.status(400).json({ error: 'Missing required parameters' });
  }

  const algorithm = VALID_ALGORITHMS.includes(type) ? type : 'HS256';
  if (type && !VALID_ALGORITHMS.includes(type)) {
    console.warn('Invalid JWT algorithm received:', type, 'Defaulting to HS256');
  }

  const payload = {
    sub: 'one-time-user',
    aud: audience,
    iss: issuer,
    iat: Math.floor(Date.now() / 1000),
    exp: Math.floor(Date.now() / 1000) + (expiresIn || 3600)
  };
  
  console.log('\n=== Token Generation Debug ===');
  console.log('Environment Variables:');
  console.log('A2A_JWT_SECRET:', process.env.A2A_JWT_SECRET ? 'Set' : 'Not Set');
  console.log('A2A_AUTH_DISABLED_AGENTS:', AUTH_DISABLED_AGENTS);
  console.log('\nRequest Parameters:');
  console.log('Agent Name:', agentName);
  console.log('Audience:', audience);
  console.log('Issuer:', issuer);
  console.log('ExpiresIn:', expiresIn || 3600);
  console.log('\nGenerated Payload:');
  console.log(JSON.stringify(payload, null, 2));
  
  try {
    const token = jwt.sign(payload, JWT_SECRET, { algorithm: 'HS256' });
    console.log('\nGenerated Token:');
    console.log(token);
    
    // Decode token to verify
    const decoded = jwt.decode(token, { complete: true });
    console.log('\nDecoded Token:');
    console.log('Header:', JSON.stringify(decoded?.header, null, 2));
    console.log('Payload:', JSON.stringify(decoded?.payload, null, 2));
    
    console.log('\n=== End Token Generation ===\n');
    return res.status(200).json({ token });
  } catch (err) {
    console.error('Failed to generate token:', err);
    return res.status(500).json({ error: 'Failed to generate token' });
  }
} 