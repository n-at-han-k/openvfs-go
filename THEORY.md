# components/services

## syncer

   The syncer only cares about operations. It doesn't have any concept of files.
   It's job is to decide what operations to schedule, and then schedule them.

## scanner

   The scanner determines the local file changes. 
   The syncer retrieves this list from the syncer.

## journal

   The journal stores information relating to the local file and operation state.
   Answer without calculation: "What files are local, what's their status, and what needs to be done".
 
   The syncer determines that file metadata needs downloading when it is missing from the journal.

# operations

  Operations that can be performed are

* upload file
* 
