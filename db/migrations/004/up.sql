insert into schema_version_history (version)
    values (4);

alter table lecturer
    add column title text null;
