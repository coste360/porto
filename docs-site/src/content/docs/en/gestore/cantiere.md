---
title: Boatyard
description: Catalogue with formulas, guided quotes, work orders, employee planner, hours, materials, final account and lifting equipment.
sidebar:
  order: 15
---

## Catalogue

*Catalogo cantiere* holds services, labour (with an hourly rate per qualification) and materials, plus **work packages**
(for example "Antifouling" = haul-out + washing + sanding + two coats + launch).

Each line can have a quantity or price **formula** using the boat's sizes (length, beam, draft, engines, hull type…),
derived variables such as the underwater hull area and variables asked to the operator (number of coats, damaged area).
Formulas have functions such as `if`, `min`, `max`, `round` and band tables; a wrong formula is flagged on save, with a
test case.

## Guided quote

*Preventivi cantiere*: 1) boat and customer, 2) work packages, services and products from the catalogue, 3) variables
required by the formulas, 4) summary with **computed** quantities and prices, editable line by line (manual changes stay
marked), 5) terms, validity and deposit. It is sent with a PDF and a link; the customer accepts from the link or at the
office, and acceptance creates the **work order** with tasks and deposit.

## Work orders

*Barche in lavorazione* (Boats in the yard) shows work orders as a board (by state) or a list: planned, in progress, on
hold, completed, delivered, invoiced. Each work order has tasks with qualification, estimated hours, dependencies,
checklists and photos; progress is the average of the tasks weighted by hours.

## Planner and workload

- *Dipendenti* (Employees): qualifications, internal hourly cost, weekly hours, absences, department; an employee can
  have a user for the phone.
- *Planner cantiere*: employees × days, with draggable tasks and checks on qualification and capacity; an automatic
  proposal assigns unplanned tasks, to be confirmed.
- *Carico di lavoro* (Workload): assigned over available hours per employee, department and qualification, with
  overloads and work orders at risk.

## Hours, materials and final account

- **Worked hours** with a timer, also from the phone and offline, or entered by hand; *Approvazione ore* confirms them
  and freezes their cost.
- **Materials** issued to and returned from the work order; **external costs** with amount, reference and mark-up.
- **Final account**: revenue and cost of hours, materials and external costs, comparison with the quote and **margin**.
- **Charges** for deposit, progress, final and balancing, which go into the "to invoice" export.

Boatyard stock is limited to the materials issued to work orders: full inventory management is not included.

## Lifting equipment

*Mezzi di sollevamento*: travel lift, crane, forklift and trailer booked in 15-minute slots with a qualified operator,
**with no overlaps** (guaranteed by the database). Haul-outs and launches are booked from the work order, from dry
storage or from a water ↔ land move; the day's calendar is also visible on the phone.
