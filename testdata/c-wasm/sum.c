int sum(const int *xs, int n) {
	int total = 0;
	for (int i = 0; i < n; i++) total += xs[i];
	return total;
}
