insert into schema_version_history (version)
    values (2);

alter table course
    alter column sws type decimal;

alter table course
    alter column credit_points type decimal;
