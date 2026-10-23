import React from 'react';
import { AppSidebar } from '@/components/layout/AppSidebar';
import { Header } from '@/components/layout/Header';
import { SidebarInset, SidebarProvider } from '@/components/ui/sidebar';
import { TooltipProvider } from '@/components/ui/tooltip';

// The signed-in page frame: the collapsible sidebar beside a scrolling column with the header on top.
export const AppShell: React.FC<{ children: React.ReactNode }> = ({ children }) => (
  <TooltipProvider>
    <SidebarProvider className="h-svh overflow-hidden bg-background font-sans text-foreground">
      <AppSidebar />
      <SidebarInset className="h-full min-w-0 overflow-y-auto">
        <Header />
        {children}
      </SidebarInset>
    </SidebarProvider>
  </TooltipProvider>
);
