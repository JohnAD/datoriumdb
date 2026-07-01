# Thought experiment

Can you make a document-oriented DB behave like a relational DB if the multi-table SQL queries were all known before hand?

That is, can you do multi-table joins that are O(1) because they are pre-calculated; which means writes become O(n) in nature?

Don't get me wrong, it is better to design the app to avoid this. But is it possible?

## Relational DB

Setting up the tables:

```SQL
CREATE TABLE users (
    user_id INT PRIMARY KEY,
    username VARCHAR(50) NOT NULL
);

CREATE TABLE categories (
    category_id INT PRIMARY KEY,
    category_name VARCHAR(50) NOT NULL
);

CREATE TABLE todos (
    todo_id INT PRIMARY KEY,
    title VARCHAR(255) NOT NULL,
    is_completed BOOLEAN DEFAULT FALSE,
    user_id INT,
    category_id INT,
    FOREIGN KEY (user_id) REFERENCES users(user_id),
    FOREIGN KEY (category_id) REFERENCES categories(category_id)
);
```

Simple Target query:

"Get all the Todo items for one user"

```SQL
SELECT
    t.title AS todo_title,
    t.is_completed,
FROM 
    todos t
INNER JOIN 
    users u ON t.user_id = u.user_id
```

Complex Target query:

"Get all the Todo items for one user whose category is less than 10 characters"

```SQL
SELECT 
    t.todo_id,
    t.title AS todo_title,
    t.is_completed,
    u.username AS created_by,
    c.category_name AS category
FROM 
    todos t
INNER JOIN 
    users u ON t.user_id = u.user_id
INNER JOIN 
    categories c ON t.category_id = c.category_id
WHERE 
    t.user_id = {target_value}
    AND
    len(c.category_name) < 10
```

## Document DB (without the precalculation)

Remember: you CANNOT gather data by anything other thatn ID ($).

Collection "User" with Schema SOT of

```txt
$:        string
username: username
```

Collection "Category" with Schema SOT of

```txt
$:            string
category_name string
```
Collection "Todo" with Schema SOT of

```txt
$:             string
title:         string
is_completed:  boolean = false
user_id        ref(User)
category_id    ref(category)
```

Example directory (no sharding):

```txt
User/
    01KVS5XW2ZWXEXNRAWDEXWQXJ1.sot.json
    01KVS4AH4J2CRH1KYGAK8WJFJ5.sot.json     # our example target user
    01KVS5Y5Z9ST4H3C5Z2EV386BA.sot.json
    References/
        todos_for_one_user/
            01KVS5XW2ZWXEXNRAWDEXWQXJ1.array.json  # contains 01KVS61GA2FK6HX9NCDKQ1B73X
            01KVS4AH4J2CRH1KYGAK8WJFJ5.array.json  # contains 01KVS61VAJD4WMT35YE2TYNK1K and 01KVS623Q8T3CWAJKQQG0SVNHF
            01KVS5Y5Z9ST4H3C5Z2EV386BA.array.json  # empty
        get_user_todos_small_category_names/
            01KVS5XW2ZWXEXNRAWDEXWQXJ1.array.json  # contains 01KVS61GA2FK6HX9NCDKQ1B73X
            01KVS4AH4J2CRH1KYGAK8WJFJ5.array.json  # ONLY contains 01KVS61VAJD4WMT35YE2TYNK1K
            01KVS5Y5Z9ST4H3C5Z2EV386BA.array.json  # empty
    Filters/
        (empty)
Category/
    000.sot.json     # "spice"
    001.sot.json     # "holiday-tomorrow"
    002.sot.json     # "foo"
    References/
        (empty)
    Filters/
        (empty)
Todo/
    01KVS61GA2FK6HX9NCDKQ1B73X.sot.json       # doesn't belong to example user
    01KVS61VAJD4WMT35YE2TYNK1K.sot.json       # our user's, category "foo"
    01KVS623Q8T3CWAJKQQG0SVNHF.sot.json       # our user's, category "hollday-tomorrow"
    References/
        (empty)
    Filters/
        (empty)
```

## The Simple Example

Reference Assignment:

```txt
name = "todos_for_one_user"
params = "{user_id: string}"
on Todo
  target User by user_id
```

"on source-Collection" means "for crud on the Collection, act on ..."

"target Collection by x" means "pre-gather references for ids on the target Collection"

So, when a Todo document is made/changed, the `Todo.$` is stored in the User collection's filters by the target `User.$`.

Usage:

```txt
  QUERY /Todo
  {
    "target": "User",
    "query": "todos_for_one_user",,
    "params": {"user_id": "01KVS4AH4J2CRH1KYGAK8WJFJ5"},
    "filters": [],
    "summary": {
      "todo_title: "Todo.title",
      "is_completed": "Todo.is_completed"
    }
  }
```

filesystem actions taken:

* 1 read of `User/References/todos_for_one_user/01KVS4AH4J2CRH1KYGAK8WJFJ5.array.json`

2 records found, so:

* 1 read of `Todo/01KVS61VAJD4WMT35YE2TYNK1K.sot.json`
* 1 read of `User/01KVS4AH4J2CRH1KYGAK8WJFJ5.sot.json`
* 1 read of `Category/002.sot.json`
* 1 read of `Todo/01KVS623Q8T3CWAJKQQG0SVNHF.sot.json`
* 1 read of `Category/001.sot.json`

## Complex Example

Reference Assignment:

```txt
name = "get_user_todos_small_category_names"
params = {user_id: string}
on Todo
  target User by user_id
      gather Category by User.category_id
      exclude LEN(Category.category_name) < 8
```

Usage:

```txt
  QUERY /Todo
  {
    "target": "User",
    "query": "get_user_todos_small_category_names",
    "params": {"user_id": "01KVS4AH4J2CRH1KYGAK8WJFJ5"},
    "filters": [],
    "summary": {
      "todo_id": "Todo.$",
      "todo_title: "Todo.title",
      "is_completed": "Todo.is_completed"
      "created_by": "User.username",
      "category": "Category.category_name",
    }
  }
```

filesystem actions taken:

* 1 read of `User/References/get_user_todos_small_category_names/01KVS4AH4J2CRH1KYGAK8WJFJ5.array.json`

1 record found, so:

* 1 read of `Todo/01KVS61VAJD4WMT35YE2TYNK1K.sot.json`
* 1 read of `User/01KVS4AH4J2CRH1KYGAK8WJFJ5.sot.json`
* 1 read of `Category/002.sot.json`

