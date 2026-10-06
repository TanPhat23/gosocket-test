import React from 'react';
import { Separator } from '@/components/ui/separator';

export const Footer: React.FC = () => {
  return (
    <>
      <Separator />
      <footer className="py-3 px-6 text-center text-xs text-muted-foreground">
        Agent Task Manager &bull; React 19 + shadcn/ui + Tailwind v4 + Zustand &bull; Self-Hostable Autonomous Control Plane
      </footer>
    </>
  );
};
