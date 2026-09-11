import { Chip } from "@heroui/react/chip";
import { type Job } from "./api";
export function JobRow({ job }: { job: Job }) {
  const color: "success" | "danger" | "accent" | "default" =
    job.status === "succeeded"
      ? "success"
      : ["failed", "interrupted"].includes(job.status)
        ? "danger"
        : job.status === "running"
          ? "accent"
          : "default";
  return (
    <div className="job">
      <div>
        <b>{job.kind}</b>{" "}
        <span className="muted">
          {job.resource} · {job.id.slice(0, 8)}
        </span>
      </div>
      <Chip size="sm" variant="soft" color={color}>
        {job.status}
      </Chip>
      <p>{job.error ?? job.stage}</p>
    </div>
  );
}
