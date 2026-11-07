// Map print (M3 D25).
/** Lens ids of the ops view → `lens` of printMap (occupancy, contract_type, vessel_type; others print plain). */
export function printLens(lens: string | undefined): string | undefined {
  switch (lens) {
    case 'occupancy':
      return 'occupancy';
    case 'contractType':
      return 'contract_type';
    case 'vesselType':
      return 'vessel_type';
    default:
      return undefined;
  }
}
