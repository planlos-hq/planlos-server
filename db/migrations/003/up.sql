insert into schema_version_history (version)
    values (3);

alter table lecturer
    add column extra_data jsonb not null default '{}'::jsonb;

alter table room
    add column extra_data jsonb not null default '{}'::jsonb;

alter table degree_program
    add column extra_data jsonb not null default '{}'::jsonb;

alter table study_group
    add column extra_data jsonb not null default '{}'::jsonb;

alter table course
    add column extra_data jsonb not null default '{}'::jsonb;
