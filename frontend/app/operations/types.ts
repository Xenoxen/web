interface mission {
  id: number;
  world_name: string;
  mission_name: string;
  mission_duration: Float64Array;
  filename: string;
  date: string;
  tag: string;
}

export type { mission };
