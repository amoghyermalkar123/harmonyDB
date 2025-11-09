## Focus of this project:
- Understanding database internals
- Understanding distributed systems


## Not focus of this project:
- Writing my own B+ Tree


Dev log [2025]:
[26/9]: Decided that i was spending too much time focusing on low level details
such as writing my own b+ tree and following a very terrible book. All I needed
was some DI material and an open source implementation to refer to.
I will be working on harmonyDB which is a read-efficient, B+ tree based database.
It will work on mmap for disk-back persistence.
inspirations: nutsdb, bbolt, mkdb

[3/11]: Finished the first draft of the raft consensus protocol. Read the paper
and implemented the algorithm from scratch. From the CRDT project i learned that
focusing on one problem/ un-answered question/ doubt/ bug at a time is the best
way to tackle a complex project like this. I am going to create a temp file where
i log every issue i have solved and open ones and tackle them one by one.

[9/11]: Finished the first draft of the database now. Implemented B+Tree, raft then
tested them together. Then I also added a file based persistence layer and then tested
the whole thing.
Problems faced: From the start of this journey to the current time as I write this devlog
I didn't face any major hurdles. I found these things clicking pretty nicely and sort of
enjoyed putting this basic database together.
Observations: To but the B+tree and the whole storage side of things together I had to read
database internals book, take a little inspiration from some oss implementations to really
write my own. What I have realized is while theory is understandable in one ways or many, I
can only really internalize something that I am reading/ learning once I implement it or write
about it or teach it in one way or the other, only then can I consider the subject `studied`.
Another observation, fundamental difference between studying and learning. Learning is about
joy, the initial spark and curiosity - a way of procrastinating for smart people. But studying
on the other hand is about commitment and laser sharp focus. To study something means to
relentlessly pursue the understanding of the knowledge that a subject entails no matter how
boring/ tedious/ difficult things might seem during the journey.
During my studying journey of distributed systems and databases I halfway
realized that I was immediately being distracted by shiny new projects/ concepts/ blogs
or ideas constantly. This happened when I was studying a complex topic or tackling a bug
or most redundantly - not knowing what to study next or add to my database project.
Which brings me to the most important skill I need to learn - planning studies. I need to
have closed loops when it comes to studying sessions. I need to know before every study session
what I want to achieve in that session and as the session ends I need to plan for the next one.
This achieves a rather simple thing - Knowing exactly what to do in the next session. After each
session ends I also know (because I plan for the next one) what I couldn't achieve in today's session.

More often than not procrastination is a result rather than a cause. I am not procrastinating because
I don't want to study. I procrastinated because I was unsure what I wanted to achieve today. Planning
is the answer. Some days the session can be short/ some days longer. Not everyday can be productive but
I need to plan everyday with clear actions so that I progress each day, instead of only some.
