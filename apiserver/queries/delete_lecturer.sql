with deleted_constraints as (
    delete from lecturer_negative_time_constraint
    where lecturer = $1
),
deleted_course_lecturers as (
    delete from course_lecturer
    where lecturer = $1)
delete from lecturer
where id = $1
