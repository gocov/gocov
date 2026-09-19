import { useMemo } from "react";
import { Link } from "react-router";
import type { TrendPoint } from "@/lib/api/types";
import { trendGeometry } from "@/lib/trend";
import { routes } from "@/lib/urls";
import "./TrendChart.css";

/**
 * Total coverage over a branch's recent commits. One thin line, two labelled
 * hairlines for the series min and max, and the gate minimum dashed across
 * when the repo has one. Every point links to the upload behind it. Fewer
 * than two points draws nothing — the page omits the section instead.
 */
export function TrendChart({
  points,
  branch,
  minCoverage,
}: {
  points: TrendPoint[];
  branch: string;
  minCoverage: number | null;
}) {
  const geo = useMemo(() => trendGeometry(points, minCoverage), [points, minCoverage]);
  if (geo === null) return null;

  return (
    <svg
      className="TrendChart"
      viewBox={`0 0 ${geo.w} ${geo.h}`}
      width="100%"
      role="img"
      aria-label={`Coverage trend on ${branch}, latest ${geo.current.label}`}
    >
      {geo.threshold !== null && (
        <>
          <line className="TrendChart__thresh" x1={geo.x0} y1={geo.threshold.y} x2={geo.x1} y2={geo.threshold.y} />
          <text className="TrendChart__label" x={geo.x1} y={geo.threshold.y} dy={-5} textAnchor="end">
            {geo.threshold.label}
          </text>
        </>
      )}
      {geo.grid.map((line) => (
        <g key={line.label}>
          <line className="TrendChart__grid" x1={geo.x0} y1={line.y} x2={geo.x1} y2={line.y} />
          <text className="TrendChart__label" x={geo.x0} dx={-6} y={line.y} textAnchor="end" dominantBaseline="middle">
            {line.label}
          </text>
        </g>
      ))}
      <path className="TrendChart__line" d={geo.path} />
      {geo.marks.map((mark, i) => (
        <Link key={`${mark.uploadId}-${i}`} to={routes.upload(mark.uploadId)} aria-label={mark.label}>
          <circle
            className={`TrendChart__point${mark.gateFailed ? " TrendChart__point--failed" : ""}`}
            cx={mark.x}
            cy={mark.y}
            r={3.5}
          >
            <title>{mark.label}</title>
          </circle>
        </Link>
      ))}
      <text className="TrendChart__label TrendChart__label--current" x={geo.current.x} y={geo.current.y} textAnchor="end">
        {geo.current.label}
      </text>
      <text className="TrendChart__label" x={geo.x0} y={geo.h} dy={-6}>
        {geo.firstDate}
      </text>
      <text className="TrendChart__label" x={geo.x1} y={geo.h} dy={-6} textAnchor="end">
        {geo.lastDate}
      </text>
    </svg>
  );
}
