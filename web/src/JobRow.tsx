import { type Job } from "./api";
export function JobRow({ job }: { job: Job }) {
  return (
    <div className="job">
      <div>
        <b>{job.kind}</b>{" "}
        <span className="muted">
          {job.resource} · {job.id.slice(0, 8)}
        </span>
      </div>
      <span className={"pill " + job.status}>{job.status}</span>
      <p>{job.error ?? job.stage}</p>
    </div>
  );
}
