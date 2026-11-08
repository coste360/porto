import { Link } from '@tanstack/react-router';
import type { ReactNode } from 'react';

import type { ReportRef } from '@porto/shared/api';

const linkClass = 'font-medium underline-offset-4 hover:underline';

/** Link from a record to its entity page (m7-plan §3.0 ref); records without a page show the text only. */
export function RefLink({
  marinaId,
  refTo,
  children,
}: {
  marinaId: string;
  refTo: ReportRef | undefined;
  children: ReactNode;
}) {
  if (!refTo?.id) return <>{children}</>;
  const id = refTo.id;
  switch (refTo.type) {
    case 'contract':
      return (
        <Link
          to="/m/$marinaId/contracts/$contractId"
          params={{ marinaId, contractId: id }}
          className={linkClass}
        >
          {children}
        </Link>
      );
    case 'transit':
      return (
        <Link
          to="/m/$marinaId/transits/$contractId"
          params={{ marinaId, contractId: id }}
          className={linkClass}
        >
          {children}
        </Link>
      );
    case 'charge':
      return (
        <Link
          to="/m/$marinaId/charges/$chargeId"
          params={{ marinaId, chargeId: id }}
          className={linkClass}
        >
          {children}
        </Link>
      );
    case 'payment':
      return (
        <Link
          to="/m/$marinaId/payments/$paymentId"
          params={{ marinaId, paymentId: id }}
          className={linkClass}
        >
          {children}
        </Link>
      );
    case 'customer':
      return (
        <Link
          to="/m/$marinaId/customers/$customerId"
          params={{ marinaId, customerId: id }}
          className={linkClass}
        >
          {children}
        </Link>
      );
    case 'vessel':
      return (
        <Link
          to="/m/$marinaId/vessels/$vesselId"
          params={{ marinaId, vesselId: id }}
          className={linkClass}
        >
          {children}
        </Link>
      );
    case 'right':
      return (
        <Link
          to="/m/$marinaId/rights/$rightId"
          params={{ marinaId, rightId: id }}
          className={linkClass}
        >
          {children}
        </Link>
      );
    case 'mandate':
      return (
        <Link
          to="/m/$marinaId/mandates/$mandateId"
          params={{ marinaId, mandateId: id }}
          className={linkClass}
        >
          {children}
        </Link>
      );
    case 'inquiry':
      return (
        <Link to="/m/$marinaId/inquiries" params={{ marinaId }} className={linkClass}>
          {children}
        </Link>
      );
    case 'cash_session':
      return (
        <Link to="/m/$marinaId/cash" params={{ marinaId }} className={linkClass}>
          {children}
        </Link>
      );
    case 'fiscal_document':
      return (
        <Link to="/m/$marinaId/fiscal" params={{ marinaId }} className={linkClass}>
          {children}
        </Link>
      );
    // M9: the maintenance and utilities pages list the tasks and consumptions (no detail route).
    case 'maintenance_task':
      return (
        <Link to="/m/$marinaId/maintenance" params={{ marinaId }} className={linkClass}>
          {children}
        </Link>
      );
    case 'utility_meter':
    case 'utility_consumption':
      return (
        <Link to="/m/$marinaId/utilities" params={{ marinaId }} className={linkClass}>
          {children}
        </Link>
      );
    default:
      return <>{children}</>;
  }
}
