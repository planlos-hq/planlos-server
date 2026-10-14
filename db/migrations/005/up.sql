insert into schema_version_history (version)
    values (5);

create table study_group_template (
    id int not null generated always as identity primary key,
    name text not null
);

create table study_group_template_course (
    template int not null references study_group_template (id),
    course int not null references course (id),
    primary key (template, course)
);
