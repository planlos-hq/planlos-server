insert into schema_version_history (version)
    values (6);

alter table course_time_slots
    add column one_off_date date null;
