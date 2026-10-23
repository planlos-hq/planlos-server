delete from ignored_lecturer_constraint_violation ilcv
where ilcv.time_slot_id = $1
    and not exists (
        select
            1
        from
            lecturer_negative_time_constraint lntc
            join course_lecturer cl on cl.lecturer = lntc.lecturer
            join course_time_slots cts on cts.course = cl.course
        where
            cts.weekday = lntc.weekday
            and cts.at < lntc.at + lntc.duration
            and lntc.at < cts.at + cts.duration
            and lntc.id = ilcv.constraint_id
            and cts.id = ilcv.time_slot_id)
returning
    constraint_id,
    time_slot_id
