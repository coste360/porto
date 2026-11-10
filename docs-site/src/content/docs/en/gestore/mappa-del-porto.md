---
title: Harbour map
description: Draw the harbour to scale with the editor - calibrated background, generators, validation, groups and categories.
sidebar:
  order: 2
---

The map is the heart of Porto: every berth, pier, area and shed is measured in **centimetres** and every boat occupies
its real footprint. It is set up in *Mappe* (Maps), with the marina configuration permission.

## Several maps per marina

You can have several maps: water area, yard, shed, parking and boxes. Each has its elements:

- **berths** on piers and quays, with fingers or mooring lines, head berths, **buoy fields**;
- **dynamic areas**: a pier side or a quay stretch where boats moor alongside by length;
- yard and shed **surfaces** made of cells, with gates and lanes, for dry storage;
- **land resources**: parking spaces, boxes, stores, lockers, cradles and trailers;
- **pedestals** with sockets (power, water) and meters, decorations and services.

## Background and scale

1. Upload an orthophoto or a plan as background.
2. Mark two points and their real distance: the editor computes the scale.
3. Rotate the background until north matches; optionally add points with geographic coordinates for georeferencing.

The background is optional: the **schematic** view works without it.

## Generators

Generators create dozens of berths at once, with a preview before applying:

- **pier** with berths on one or both sides, also growing towards the head, a finger every N berths or mooring lines,
  head berths, numbering and prefix;
- **berths along a quay**;
- **dynamic area** on a pier side or quay stretch;
- **buoy field** in a grid or staggered, with the swing radius;
- **grid of parking spaces and boxes**;
- **yard or shed** made of cells with gates and lanes.

The editor snaps to the grid and to structures and has multiple selection, align and distribute, duplicate, renumber,
undo and redo.

## Validation

On save Porto checks the map: it **blocks** duplicate codes and overlapping berths and **warns** about berths below the
minimum size, without depth or without a price category. A berth with future occupancies is not deleted but
deactivated, and Porto lists the contracts and transits to move; resizing it requires checking the boats assigned to
it.

## Groups and categories

- **Groups** (for example "Pier B" or "Transits") gather berths, areas and resources and are used in price lists,
  online quotas, reports, filters and communications. A berth can belong to several groups.
- **Price categories** are assigned per berth or in bulk from the selection.

## Import and print

You can import the berth list from CSV (code, pier, sizes, depth, category), handy when coming from another system, and
print the map to scale with the legend. Every save creates a new version of the map.
