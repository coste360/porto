import type { ReactNode } from 'react';

import { BrandLogo } from '@porto/shared/ui/patterns';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@porto/shared/ui/card';

/** Centered card used by the sign-in, recovery and enrolment pages. */
export function AuthCard({
  title,
  description,
  children,
}: {
  title: ReactNode;
  description?: ReactNode;
  children: ReactNode;
}) {
  return (
    <main className="bg-muted flex min-h-svh flex-col items-center justify-center gap-6 p-6">
      <BrandLogo variant="horizontal" size="lg" />
      <Card className="w-full max-w-md">
        <CardHeader>
          <CardTitle className="text-xl">{title}</CardTitle>
          {description ? <CardDescription>{description}</CardDescription> : null}
        </CardHeader>
        <CardContent>{children}</CardContent>
      </Card>
    </main>
  );
}
