int internal_add(int a, int b) {
    return a + b;
}

__attribute__((export_name("add")))
int add(int a, int b)
{
    int res;
    res = internal_add(a, b);
    return res;
}