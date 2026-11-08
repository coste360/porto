import type { Page, Route } from "@playwright/test";

// Storage fixtures of the M9 PLANNER specs (m9-plan §14.5): the storage endpoints are answered by page.route with
// a 20 × 10 shed (1 m cells), a door on the left side, two boats and one draft scenario, so the planner,
// scenario, contract and phone screens are exercised whatever the server data. Every request the page makes is
// recorded for the assertions.

export const AREA_ID = "01900000-0000-7000-8000-0000000000a1";
export const SCENARIO_ID = "01900000-0000-7000-8000-0000000000c1";

export function today(): string {
  return new Date().toISOString().slice(0, 10);
}

export function addDays(d: string, n: number): string {
  return new Date(Date.parse(`${d}T00:00:00Z`) + n * 86_400_000)
    .toISOString()
    .slice(0, 10);
}

const area = {
  id: AREA_ID,
  map_id: "m1",
  map_kind: "shed",
  code: "CAPANNONE",
  name: "Capannone E2E",
  cell_cm: 100,
  cols: 20,
  rows: 10,
  usable_cells: 200,
  usable_cm2: 2_000_000,
  occupied_cells_today: 50,
  boats_today: 2,
  exits_blocked_next_30: 1,
  draft_scenarios: 1,
  gates_valid: true,
};

const vessel = (id: string, name: string) => ({
  id,
  name,
  loa_cm: 800,
  beam_cm: 300,
  hull_type: "monohull_sail",
});

export function plan() {
  const t = today();
  return {
    area,
    from: t,
    to: addDays(t, 30),
    gates: [{ index: 0, cells: [80, 100], valid: true }],
    obstacles: [],
    items: [
      {
        allocation_id: "al-a",
        kind: "contract",
        vessel: vessel("v-a", "Aria"),
        x_cm: 0,
        y_cm: 0,
        rotation_dd: 0,
        cells: [],
        first_night: addDays(t, -30),
        last_night: addDays(t, 120),
        arrival_on: addDays(t, -30),
        exit_on: addDays(t, 2),
        version: 3,
      },
      {
        allocation_id: "al-b",
        kind: "contract",
        vessel: vessel("v-b", "Bora"),
        x_cm: 1000,
        y_cm: 500,
        rotation_dd: 0,
        cells: [],
        first_night: addDays(t, -30),
        last_night: addDays(t, 120),
        arrival_on: addDays(t, -30),
        exit_on: addDays(t, 121),
        version: 1,
      },
    ],
    access: [
      {
        allocation_id: "al-a",
        event: "exit",
        on: addDays(t, 2),
        accessible: false,
        blocking: ["al-b"],
      },
    ],
  };
}

export const indicators = {
  usable_cm2: 2_000_000,
  used_hull_cm2: 480_000,
  used_footprint_cm2: 600_000,
  moves: 1,
  unplaced: 0,
  overlaps: 0,
  exits_blocked: 0,
  arrivals_blocked: 0,
  largest_free_rect_cm2: 900_000,
  blocked_by_date: [],
};

export function scenario() {
  const p = plan();
  return {
    scenario: {
      id: SCENARIO_ID,
      area_id: AREA_ID,
      name: "Inverno E2E",
      author_user_id: "u1",
      from_on: p.from,
      to_on: p.to,
      status: "draft",
      seed: 42,
      stale: false,
      version: 4,
      indicators,
      created_at: `${p.from}T08:00:00Z`,
      updated_at: `${p.from}T08:00:00Z`,
    },
    items: p.items.map((i) => ({
      allocation_id: i.allocation_id,
      kind: i.kind,
      origin: "area",
      vessel: i.vessel,
      x_cm: i.x_cm,
      y_cm: i.y_cm,
      rotation_dd: i.rotation_dd,
      pinned: false,
      first_night: i.first_night,
      last_night: i.last_night,
      arrival_on: i.arrival_on,
      exit_on: i.exit_on,
      base_version: i.version,
    })),
    moves: [
      {
        seq: 1,
        allocation_id: "al-b",
        kind: "move",
        to: {
          resource_type: "dynamic_area",
          resource_id: AREA_ID,
          x_cm: 1500,
          y_cm: 0,
        },
        move_on: p.from,
      },
    ],
    issues: [],
    access: [],
  };
}

export function movements() {
  return {
    date: today(),
    items: [
      {
        id: "mv-1",
        area_id: AREA_ID,
        allocation_id: "al-b",
        vessel: vessel("v-b", "Bora"),
        seq: 1,
        move_on: today(),
        from: {
          resource_type: "dynamic_area",
          resource_id: AREA_ID,
          x_cm: 1000,
          y_cm: 500,
          rotation_dd: 0,
        },
        to: {
          resource_type: "dynamic_area",
          resource_id: AREA_ID,
          x_cm: 1500,
          y_cm: 0,
          rotation_dd: 0,
        },
        equipment_hint: "cradle_mover",
        temporary: false,
        status: "pending",
        version: 1,
      },
    ],
    warnings: [
      {
        allocation_id: "al-a",
        event: "exit",
        on: today(),
        accessible: false,
        blocking: ["al-b"],
      },
    ],
  };
}

export type Recorded = {
  method: string;
  path: string;
  body: unknown;
  headers: Record<string, string>;
};

/**
 * Answers the storage endpoints of the marina; `apply` decides the apply answer (default: 409 with two
 * conflicts). Returns the recorded requests.
 */
/** A boat on another surface area that the "Da collocare" tray accepts. */
export const trayCandidate = {
  allocation_id: "al-yard",
  vessel: {
    id: "v-yard",
    name: "Grecale",
    loa_cm: 700,
    beam_cm: 250,
    hull_type: "monohull_motor",
  },
  resource_type: "dynamic_area",
  resource_id: "area-yard",
  resource_code: "PIAZZALE",
  first_night: "2026-11-08",
  last_night: "2027-04-30",
};

export async function mockStorage(
  page: Page,
  marinaId: string,
  opts: { apply?: { status: number; body: unknown } } = {},
): Promise<Recorded[]> {
  const calls: Recorded[] = [];
  const base = `/api/admin/v1/marinas/${marinaId}`;
  const json = (route: Route, body: unknown, status = 200) =>
    route.fulfill({
      status,
      contentType:
        status >= 400 ? "application/problem+json" : "application/json",
      body: JSON.stringify(body),
    });
  await page.route(
    (u) =>
      u.pathname.startsWith(`${base}/storage`) ||
      u.pathname === `${base}/dynamic-areas/${AREA_ID}/layout`,
    async (route) => {
      const req = route.request();
      const path = new URL(req.url()).pathname.slice(base.length);
      const body = req.postData() ? JSON.parse(req.postData()!) : undefined;
      calls.push({ method: req.method(), path, body, headers: req.headers() });
      if (path === "/storage/areas") return json(route, { items: [area] });
      if (path === `/storage/areas/${AREA_ID}/plan`) return json(route, plan());
      if (path === `/storage/areas/${AREA_ID}/evaluate`)
        return json(route, { indicators, access: [], issues: [] });
      if (path === `/storage/areas/${AREA_ID}/arrange`)
        return json(route, {
          items: [
            { allocation_id: "al-a", x_cm: 1200, y_cm: 100, rotation_dd: 0 },
            { allocation_id: "al-b", x_cm: 1000, y_cm: 500, rotation_dd: 0 },
          ],
          indicators,
          access: [],
          seed: 7,
          iterations: 100,
          stopped_by: "optimal",
        });
      if (path === `/dynamic-areas/${AREA_ID}/layout`)
        return json(route, { moved: [] });
      if (path === "/storage/scenarios" && req.method() === "GET")
        return json(route, { items: [scenario().scenario], next_cursor: null });
      if (path === `/storage/scenarios/${SCENARIO_ID}`)
        return json(route, scenario());
      if (path === `/storage/scenarios/${SCENARIO_ID}/tray-candidates`)
        return json(route, { items: [trayCandidate] });
      if (
        path === `/storage/scenarios/${SCENARIO_ID}/items` &&
        req.method() === "POST"
      )
        return json(route, scenario());
      if (path === `/storage/scenarios/${SCENARIO_ID}/apply`) {
        const a = opts.apply ?? {
          status: 409,
          body: {
            type: "scenario_conflicts",
            title: "conflicts",
            status: 409,
            details: {
              conflicts: [
                { allocation_id: "al-a", reason: "stale" },
                { allocation_id: "al-b", reason: "resource_taken" },
              ],
            },
          },
        };
        return json(route, a.body, a.status);
      }
      if (path === "/storage/movements") return json(route, movements());
      if (path === "/storage/movements/mv-1/done")
        return json(route, {
          ...movements().items[0],
          status: "done",
          version: 2,
        });
      if (path === "/storage/contracts" && req.method() === "POST")
        return json(
          route,
          { type: "storage_no_space", title: "no space", status: 409 },
          409,
        );
      return route.fallback();
    },
  );
  return calls;
}
