delete from ignored_course_lecturer_violation iclv
where not exists (
        select
            1
        from
            course_lecturer cl
            join lecturer l on l.id = cl.lecturer
        where
            cl.course = iclv.course
        group by
            cl.course
        having
            count(*) filter (where l.kind != 'lab_engineer') = 0
            and count(*) filter (where l.kind = 'lab_engineer') > 0);
