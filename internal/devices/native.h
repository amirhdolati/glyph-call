#include <stdint.h>
int ac_camera_count(void);
const char *ac_camera_name(int index);
int ac_camera_start(int index, char *error, int error_size);
int ac_camera_read(void *data, int capacity, int *width, int *height, int *stride);
void ac_camera_stop(void);
int ac_audio_start(char *error, int error_size);
int ac_audio_read(void *data, int size);
void ac_audio_write(const void *data, int size);
void ac_audio_stop(void);
