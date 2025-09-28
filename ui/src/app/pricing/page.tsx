'use client';

import { useState } from 'react';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Label } from '@/components/ui/label';
import { Input } from '@/components/ui/input';
import { ProtectedRoute } from '@/components/ProtectedRoute';

export default function PricingPage() {
  const [plan, setPlan] = useState<'community' | 'pro' | 'enterprise'>('pro');
  const [method, setMethod] = useState<'card' | 'paypal'>('card');
  const [submitting, setSubmitting] = useState(false);

  const handleCheckout = async (e: React.FormEvent) => {
    e.preventDefault();
    setSubmitting(true);
    try {
      // Placeholder: integrate with your payments provider here
      await new Promise((r) => setTimeout(r, 800));
      alert(`Subscribed to ${plan.toUpperCase()} via ${method === 'card' ? 'Credit Card' : 'PayPal'}.`);
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <ProtectedRoute>
    <div className="max-w-6xl mx-auto px-6 py-12 space-y-10">
      <div className="text-center space-y-3">
        <h1 className="text-4xl md:text-5xl font-bold">Pricing</h1>
        <p className="text-muted-foreground max-w-2xl mx-auto">Simple, transparent plans for every stage.</p>
      </div>

      <div className="grid md:grid-cols-3 gap-6">
        <Card className={plan === 'community' ? 'ring-2 ring-primary' : ''}>
          <CardHeader>
            <CardTitle>Community</CardTitle>
            <CardDescription>Open-source. Ideal for solo builders.</CardDescription>
          </CardHeader>
          <CardContent>
            <div className="text-3xl font-bold">$0</div>
            <ul className="mt-4 space-y-2 text-sm text-muted-foreground">
              <li>All core features</li>
              <li>Local tool server</li>
              <li>Community support</li>
            </ul>
            <Button className="mt-6 w-full" variant={plan === 'community' ? 'default' : 'outline'} onClick={() => setPlan('community')}>Choose</Button>
          </CardContent>
        </Card>

        <Card className={plan === 'pro' ? 'ring-2 ring-primary' : ''}>
          <CardHeader>
            <CardTitle>Pro</CardTitle>
            <CardDescription>For startups and teams shipping to prod.</CardDescription>
          </CardHeader>
          <CardContent>
            <div className="text-3xl font-bold">$49<span className="text-base font-normal">/mo</span></div>
            <ul className="mt-4 space-y-2 text-sm text-muted-foreground">
              <li>Everything in Community</li>
              <li>Hosted tool servers</li>
              <li>Observability & analytics</li>
            </ul>
            <Button className="mt-6 w-full" variant={plan === 'pro' ? 'default' : 'outline'} onClick={() => setPlan('pro')}>Choose</Button>
          </CardContent>
        </Card>

        <Card className={plan === 'enterprise' ? 'ring-2 ring-primary' : ''}>
          <CardHeader>
            <CardTitle>Enterprise</CardTitle>
            <CardDescription>Security, SSO, SLAs, and premium support.</CardDescription>
          </CardHeader>
          <CardContent>
            <div className="text-3xl font-bold">Custom</div>
            <ul className="mt-4 space-y-2 text-sm text-muted-foreground">
              <li>Private cloud or on-prem</li>
              <li>Advanced governance</li>
              <li>Dedicated solutions</li>
            </ul>
            <Button className="mt-6 w-full" variant={plan === 'enterprise' ? 'default' : 'outline'} onClick={() => setPlan('enterprise')}>Choose</Button>
          </CardContent>
        </Card>
      </div>

      {plan !== 'community' && (
        <form onSubmit={handleCheckout} className="max-w-2xl mx-auto">
          <Card>
            <CardHeader>
              <CardTitle>Checkout</CardTitle>
              <CardDescription>Select payment method and complete your subscription.</CardDescription>
            </CardHeader>
            <CardContent className="space-y-6">
              <div className="flex items-center gap-4">
                <label className="flex items-center gap-2 cursor-pointer">
                  <input type="radio" name="method" value="card" checked={method === 'card'} onChange={() => setMethod('card')} />
                  <span>Credit/Debit Card</span>
                </label>
                <label className="flex items-center gap-2 cursor-pointer">
                  <input type="radio" name="method" value="paypal" checked={method === 'paypal'} onChange={() => setMethod('paypal')} />
                  <span>PayPal</span>
                </label>
              </div>

              {method === 'card' ? (
                <div className="grid md:grid-cols-2 gap-4">
                  <div className="space-y-2 md:col-span-2">
                    <Label htmlFor="cardName">Name on card</Label>
                    <Input id="cardName" placeholder="Ada Lovelace" required />
                  </div>
                  <div className="space-y-2 md:col-span-2">
                    <Label htmlFor="cardNumber">Card number</Label>
                    <Input id="cardNumber" inputMode="numeric" placeholder="4242 4242 4242 4242" required />
                  </div>
                  <div className="space-y-2">
                    <Label htmlFor="expiry">Expiry</Label>
                    <Input id="expiry" placeholder="MM/YY" required />
                  </div>
                  <div className="space-y-2">
                    <Label htmlFor="cvc">CVC</Label>
                    <Input id="cvc" placeholder="123" required />
                  </div>
                </div>
              ) : (
                <div className="space-y-2">
                  <p className="text-sm text-muted-foreground">You will be redirected to PayPal to complete your purchase.</p>
                  <Button type="submit" variant="outline" disabled={submitting}>{submitting ? 'Redirecting…' : 'Continue with PayPal'}</Button>
                </div>
              )}

              {method === 'card' && (
                <div className="flex justify-end">
                  <Button type="submit" disabled={submitting}>{submitting ? 'Processing…' : `Subscribe to ${plan === 'pro' ? 'Pro' : 'Enterprise'}`}</Button>
                </div>
              )}
            </CardContent>
          </Card>
        </form>
      )}

      {plan === 'community' && (
        <div className="text-center">
          <Button asChild>
            <a href="/signup">Start Free</a>
          </Button>
        </div>
      )}
    </div>
    </ProtectedRoute>
  );
}
