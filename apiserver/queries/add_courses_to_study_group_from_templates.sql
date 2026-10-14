with templates as (
    select
        unnest as template
    from
        unnest($2::integer[])
),
template_courses as (
    select distinct
        tc.course
    from
        study_group_template_course tc
        join templates t using (template))
    insert into course_study_group (course, study_group)
    select
        course,
        $1
    from
        template_courses
    on conflict (course,
        study_group)
        do nothing
    returning
        course,
        study_group;
