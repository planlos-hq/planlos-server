with deleted_course_assignments as (
    delete from study_group_template_course
    where template = $1)
delete from study_group_template
where id = $1
