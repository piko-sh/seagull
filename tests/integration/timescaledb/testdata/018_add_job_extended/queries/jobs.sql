-- sgl.query(name: ScheduleJob, command: one)
WITH scheduled AS (
    SELECT add_job(
        'my_proc',
        INTERVAL '1 day',
        config         => '{}'::jsonb,
        fixed_schedule => true,
        timezone       => 'UTC'
    ) AS raw_job_id
)
SELECT raw_job_id::integer AS job_id FROM scheduled;

-- sgl.query(name: DeleteJob, command: exec)
-- $1 as sgl.param(jobID, nullable: false)
SELECT delete_job($1);
